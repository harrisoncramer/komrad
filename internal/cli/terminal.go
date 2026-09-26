package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"sync"
	"syscall"
	"time"

	"komrad/lib/xhttp"
	terminalv1 "komrad/proto/gen/terminal/v1"
	"komrad/proto/gen/terminal/v1/terminalv1connect"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const (
	// redialFor is how long a dropped attach is dialled again for before giving up.
	redialFor = 2 * time.Minute
	// firstRedialWait is how long the first redial waits, doubling up to lastRedialWait.
	firstRedialWait = 250 * time.Millisecond
	lastRedialWait  = 5 * time.Second
)

var droppedCodes = []connect.Code{connect.CodeUnavailable, connect.CodeUnknown, connect.CodeDeadlineExceeded, connect.CodeAborted}

var (
	errEndedEarly = errors.New("the terminal ended without an exit status")
	errNotReady   = errors.New("the engine did not say it was ready")
)

func terminalCmd(state *app) *cobra.Command {
	var addr string

	cmd := &cobra.Command{
		Use:   "terminal AGENT",
		Short: "Attach to an agent's terminal on the engine",
		Long: `Attach to an agent's terminal on the engine.

The terminal is carried to this one over the engine's api, so it works the same
whether the engine is this machine or another one. AGENT is the agent's id, and
the agent has to be live and hold a pane. Attaching detaches any other terminal
attached to the same agent. A dropped connection is dialled again, and since
tmux keeps the session nothing is lost.`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if addr == "" {
				dialed, release, err := state.dialAPI(ctx)
				if err != nil {
					return err
				}
				defer release()

				addr = dialed
			}

			client := terminalv1connect.NewTerminalServiceClient(xhttp.NewClientOver(xhttp.NewStreamTransport(), 0), "http://"+addr)
			code, err := runConsole(ctx, runConsoleParams{
				Dial:   dialAttach(client, args[0], state.env.Term),
				Redial: redialFor,
			})
			if err != nil {
				return err
			}

			if code != 0 {
				return &ExitError{Code: code}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&addr, "addr", "", "address of the engine's api, reached as it is rather than through a forward of its own")

	return cmd
}

func dialAttach(client terminalv1connect.TerminalServiceClient, agentID, term string) dialFunc {
	return func(ctx context.Context, size *terminalv1.Size) (terminalConn, error) {
		conn := &streamConn[terminalv1.AttachRequest, terminalv1.AttachResponse]{
			stream: client.Attach(ctx),
			input: func(input []byte) *terminalv1.AttachRequest {
				return &terminalv1.AttachRequest{Message: &terminalv1.AttachRequest_Input{Input: input}}
			},
			resize: func(size *terminalv1.Size) *terminalv1.AttachRequest {
				return &terminalv1.AttachRequest{Message: &terminalv1.AttachRequest_Resize{Resize: size}}
			},
			read: func(response *terminalv1.AttachResponse) received {
				return received{ready: response.GetReady() != nil, output: response.GetOutput(), exit: response.GetExit()}
			},
		}

		open := &terminalv1.AttachRequest{Message: &terminalv1.AttachRequest_Open{Open: &terminalv1.OpenAttach{
			AgentId: agentID,
			Size:    size,
			Term:    term,
		}}}
		if err := conn.open(open); err != nil {
			return nil, fmt.Errorf("failed to attach to agent %s: %w", agentID, err)
		}

		return conn, nil
	}
}

// received is one message from the engine, whichever kind of terminal it came from.
type received struct {
	ready  bool
	output []byte
	exit   *terminalv1.Exit
}

// terminalConn is one open terminal stream.
type terminalConn interface {
	sendInput(input []byte) error
	sendResize(size *terminalv1.Size) error
	closeInput() error
	receive() (received, error)
	close()
}

// dialFunc opens a terminal stream at a size and waits for the engine to say it is ready.
type dialFunc func(ctx context.Context, size *terminalv1.Size) (terminalConn, error)

// streamConn is a terminal stream spoken in one service's messages.
type streamConn[Req, Res any] struct {
	stream *connect.BidiStreamForClient[Req, Res]
	input  func(input []byte) *Req
	resize func(size *terminalv1.Size) *Req
	read   func(response *Res) received

	sending sync.Mutex
}

// open sends the opening message and waits for ready.
func (c *streamConn[Req, Res]) open(open *Req) error {
	if err := c.send(open); err != nil && !errors.Is(err, io.EOF) {
		c.close()

		return err
	}

	first, err := c.stream.Receive()
	if err != nil {
		c.close()

		return err //nolint:wrapcheck // the caller names what was being opened
	}

	if !c.read(first).ready {
		c.close()

		return errNotReady
	}

	return nil
}

func (c *streamConn[Req, Res]) send(request *Req) error {
	c.sending.Lock()
	defer c.sending.Unlock()

	return c.stream.Send(request) //nolint:wrapcheck // the caller hears about a failed stream from receive
}

func (c *streamConn[Req, Res]) sendInput(input []byte) error {
	return c.send(c.input(input))
}

func (c *streamConn[Req, Res]) sendResize(size *terminalv1.Size) error {
	return c.send(c.resize(size))
}

func (c *streamConn[Req, Res]) closeInput() error {
	c.sending.Lock()
	defer c.sending.Unlock()

	return c.stream.CloseRequest() //nolint:wrapcheck // the caller hears about a failed stream from receive
}

func (c *streamConn[Req, Res]) receive() (received, error) {
	response, err := c.stream.Receive()
	if errors.Is(err, io.EOF) {
		return received{}, errEndedEarly
	}

	if err != nil {
		return received{}, err //nolint:wrapcheck // the caller tells a dropped stream apart by its code
	}

	return c.read(response), nil
}

func (c *streamConn[Req, Res]) close() {
	_ = c.stream.CloseResponse() //nolint:errcheck // the stream is being let go
}

type runConsoleParams struct {
	Dial   dialFunc
	Redial time.Duration
}

// runConsole runs a terminal on this process's own terminal, raw while it runs and resized with it.
func runConsole(ctx context.Context, params runConsoleParams) (int, error) {
	fd := int(os.Stdin.Fd()) //nolint:gosec // a descriptor fits an int
	isConsole := term.IsTerminal(fd)

	resized := make(chan os.Signal, 1)
	signal.Notify(resized, syscall.SIGWINCH)
	defer signal.Stop(resized)

	return runTerminal(ctx, runTerminalParams{
		Dial:    params.Dial,
		Redial:  params.Redial,
		In:      os.Stdin,
		Out:     os.Stdout,
		Resized: resized,
		Size: func() *terminalv1.Size {
			return readConsoleSize(fd)
		},
		Ready: func() (func(), error) {
			if !isConsole {
				return func() {}, nil
			}

			previous, err := term.MakeRaw(fd)
			if err != nil {
				return nil, fmt.Errorf("failed to put the terminal in raw mode: %w", err)
			}

			return func() { _ = term.Restore(fd, previous) }, nil //nolint:errcheck // the terminal is being handed back on the way out
		},
	})
}

type runTerminalParams struct {
	Dial dialFunc
	// Redial is how long a dropped stream is dialled again for, never when zero.
	Redial  time.Duration
	In      io.Reader
	Out     io.Writer
	Size    func() *terminalv1.Size
	Resized <-chan os.Signal
	Ready   func() (func(), error)
}

// runTerminal opens a terminal and carries In to it and its output to Out until it exits.
func runTerminal(ctx context.Context, params runTerminalParams) (int, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	conn, err := params.Dial(ctx, params.Size())
	if err != nil {
		return 0, err
	}
	defer func() { conn.close() }()

	restore, err := params.Ready()
	if err != nil {
		return 0, err
	}
	defer restore()

	inputs := make(chan []byte)
	go readInput(ctx, params.In, inputs)

	for {
		code, err := carry(ctx, conn, params, inputs)
		if err == nil {
			return code, nil
		}

		if params.Redial == 0 || !isDropped(ctx, err) {
			return 0, err
		}

		next, err := redial(ctx, params)
		if err != nil {
			return 0, err
		}

		conn.close()
		conn = next
	}
}

// carry runs one stream until the terminal exits or the stream fails.
func carry(ctx context.Context, conn terminalConn, params runTerminalParams, inputs <-chan []byte) (int, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go forward(ctx, conn, params, inputs)

	for {
		message, err := conn.receive()
		if err != nil {
			return 0, err
		}

		if len(message.output) > 0 {
			if _, err := params.Out.Write(message.output); err != nil {
				return 0, fmt.Errorf("failed to draw the terminal: %w", err)
			}
		}

		if message.exit != nil {
			return int(message.exit.GetCode()), nil
		}
	}
}

// forward sends what is typed and each resize to one stream, closing its input when the typing
// ends.
func forward(ctx context.Context, conn terminalConn, params runTerminalParams, inputs <-chan []byte) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-params.Resized:
			if err := conn.sendResize(params.Size()); err != nil {
				return
			}
		case input, ok := <-inputs:
			if !ok {
				_ = conn.closeInput() //nolint:errcheck // a stream that failed is heard about by receive
				inputs = nil

				continue
			}

			if err := conn.sendInput(input); err != nil {
				return
			}
		}
	}
}

// readInput hands on what is read from in, closing inputs when in ends.
func readInput(ctx context.Context, in io.Reader, inputs chan<- []byte) {
	defer close(inputs)

	buffer := make([]byte, 32*1024)
	for {
		n, err := in.Read(buffer)
		if n > 0 {
			select {
			case inputs <- append([]byte(nil), buffer[:n]...):
			case <-ctx.Done():
				return
			}
		}

		if err != nil {
			return
		}
	}
}

// redial dials again until a stream opens, the engine refuses it, or params.Redial runs out.
func redial(ctx context.Context, params runTerminalParams) (terminalConn, error) {
	deadline := time.Now().Add(params.Redial)
	wait := firstRedialWait

	for {
		conn, err := params.Dial(ctx, params.Size())
		if err == nil {
			return conn, nil
		}

		if !isDropped(ctx, err) || time.Now().After(deadline) {
			return nil, err
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("failed to dial the engine again: %w", ctx.Err())
		case <-time.After(wait):
		}

		wait = min(wait*2, lastRedialWait)
	}
}

// isDropped reports whether a stream failed from a lost connection.
func isDropped(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}

	if errors.Is(err, errEndedEarly) {
		return true
	}

	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		return false
	}

	return slices.Contains(droppedCodes, connectErr.Code())
}

func readConsoleSize(fd int) *terminalv1.Size {
	cols, rows, err := term.GetSize(fd)
	if err != nil || rows <= 0 || cols <= 0 {
		return &terminalv1.Size{Rows: 24, Cols: 80}
	}

	return &terminalv1.Size{Rows: uint32(rows), Cols: uint32(cols)} //nolint:gosec // both are positive
}
