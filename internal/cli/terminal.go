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

	"komrad/internal/engine"
	"komrad/lib/clock"
	"komrad/lib/xhttp"
	terminalv1 "komrad/proto/gen/terminal/v1"
	"komrad/proto/gen/terminal/v1/terminalv1connect"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const (
	// redialFor is how long to redial a dropped attach before giving up.
	redialFor = 2 * time.Minute
	// keepaliveInterval is how often an open terminal sends the engine a message, well inside the minute a
	// proxy in front of the engine lets a request go silent.
	keepaliveInterval = 20 * time.Second
	// firstRedialWait is how long the first redial waits, doubling up to lastRedialWait.
	firstRedialWait = 250 * time.Millisecond
	lastRedialWait  = 5 * time.Second
	// signInCheckInterval is how often a terminal waiting on a sign-in looks for one.
	signInCheckInterval = time.Second
)

var (
	errEndedEarly = errors.New("the terminal ended without an exit status")
	errNotReady   = errors.New("the engine did not say it was ready")
)

// unreachableCodes are the codes a dial fails with when it cannot reach the engine.
var unreachableCodes = []connect.Code{connect.CodeUnavailable, connect.CodeDeadlineExceeded, connect.CodeAborted}

// droppedCodes are what a carried stream fails with when its connection is lost.
var droppedCodes = append(slices.Clone(unreachableCodes), connect.CodeUnknown, connect.CodeInternal, connect.CodeInvalidArgument)

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
	sendToken(token string) error
	sendKeepalive() error
	closeInput() error
	receive() (received, error)
	close()
}

// dialFunc opens a terminal stream at a size and waits for the engine to say it is ready.
type dialFunc func(ctx context.Context, size *terminalv1.Size) (terminalConn, error)

// streamConn is a terminal stream spoken in one service's messages.
type streamConn[Req, Res any] struct {
	stream  *connect.BidiStreamForClient[Req, Res]
	input   func(input []byte) *Req
	resize  func(size *terminalv1.Size) *Req
	token   func(token string) *Req
	read    func(response *Res) received
	sending sync.Mutex
}

func terminalCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "terminal AGENT",
		Short: "Attach to an agent's terminal on the engine",
		Long: `Attach to an agent's terminal on the engine.

The terminal is carried to this one over the engine's api, so it works the same
whether the engine is this machine or another one. AGENT is the agent's id, and
the agent has to be live and hold a pane. Attaching detaches any other terminal
attached to the same agent. A dropped connection is dialled again, and since
tmux keeps the session nothing is lost. Opened before this machine signs in to
an engine at its url, it says so and attaches once komrad login runs.`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			client := terminalv1connect.NewTerminalServiceClient(xhttp.NewClientOver(state.signTransport(xhttp.NewStreamTransport()), 0), state.getAPIBase())
			code, err := runConsole(ctx, runConsoleParams{
				Dial:   dialAttach(client, args[0], state.env.Term),
				Redial: redialFor,
				Token:  state.getTokenSource(),
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
			token: func(token string) *terminalv1.AttachRequest {
				return &terminalv1.AttachRequest{Message: &terminalv1.AttachRequest_Token{Token: token}}
			},
			read: func(response *terminalv1.AttachResponse) received {
				return received{
					ready:  response.GetReady() != nil,
					output: response.GetOutput(),
					exit:   response.GetExit(),
				}
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

// open sends the opening message and waits for ready.
func (c *streamConn[Req, Res]) open(open *Req) error {
	if err := c.send(open); err != nil && !errors.Is(err, io.EOF) {
		c.close()

		return fmt.Errorf("failed to send the opening message: %w", err)
	}

	first, err := c.stream.Receive()
	if err != nil {
		c.close()

		return fmt.Errorf("failed to hear whether the terminal opened: %w", err)
	}

	if !c.read(first).ready {
		c.close()

		return fmt.Errorf("failed to open the terminal: %w", errNotReady)
	}

	return nil
}

func (c *streamConn[Req, Res]) send(request *Req) error {
	c.sending.Lock()
	defer c.sending.Unlock()

	if err := c.stream.Send(request); err != nil {
		return fmt.Errorf("failed to send to the terminal: %w", err)
	}

	return nil
}

func (c *streamConn[Req, Res]) sendInput(input []byte) error {
	return c.send(c.input(input))
}

func (c *streamConn[Req, Res]) sendResize(size *terminalv1.Size) error {
	return c.send(c.resize(size))
}

func (c *streamConn[Req, Res]) sendToken(token string) error {
	return c.send(c.token(token))
}

func (c *streamConn[Req, Res]) sendKeepalive() error {
	return c.send(new(Req))
}

func (c *streamConn[Req, Res]) closeInput() error {
	c.sending.Lock()
	defer c.sending.Unlock()

	if err := c.stream.CloseRequest(); err != nil {
		return fmt.Errorf("failed to close the terminal's input: %w", err)
	}

	return nil
}

func (c *streamConn[Req, Res]) receive() (received, error) {
	response, err := c.stream.Receive()
	if errors.Is(err, io.EOF) {
		return received{}, errEndedEarly
	}

	if err != nil {
		return received{}, fmt.Errorf("failed to read the next message: %w", err)
	}

	return c.read(response), nil
}

func (c *streamConn[Req, Res]) close() {
	_ = c.stream.CloseResponse() //nolint:errcheck // the stream is being let go
}

type runConsoleParams struct {
	Dial   dialFunc
	Redial time.Duration
	// Token is a fresh token for the engine, or nil when the engine takes none.
	Token func(ctx context.Context) (string, error)
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
		Token:   params.Token,
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

			return func() {
				_ = term.Restore(fd, previous) //nolint:errcheck // the terminal is being handed back on the way out
			}, nil
		},
	})
}

type runTerminalParams struct {
	Dial dialFunc
	// Redial is how long to redial a dropped stream, and never when zero.
	Redial time.Duration
	In     io.Reader
	// Token is a fresh token for the engine, or nil when the engine takes none.
	Token   func(ctx context.Context) (string, error)
	Out     io.Writer
	Size    func() *terminalv1.Size
	Resized <-chan os.Signal
	Ready   func() (func(), error)
	// KeepaliveInterval is how often the stream sends the engine a message, or keepaliveInterval when zero.
	KeepaliveInterval time.Duration
}

// runTerminal opens a terminal and carries In to it and its output to Out until it exits.
func runTerminal(ctx context.Context, params runTerminalParams) (int, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := awaitSignIn(ctx, params); err != nil {
		return 0, err
	}

	conn, err := params.Dial(ctx, params.Size())
	if err != nil {
		return 0, fmt.Errorf("failed to open the terminal: %w", err)
	}
	defer func() { conn.close() }()

	restore, err := params.Ready()
	if err != nil {
		return 0, fmt.Errorf("failed to ready this terminal: %w", err)
	}
	defer restore()

	inputs := make(chan []byte)
	go readInput(ctx, params.In, inputs)

	var unsent []byte
	for {
		code, err := carry(ctx, carryParams{
			Conn:     conn,
			Inputs:   inputs,
			Unsent:   &unsent,
			Terminal: params,
		})
		if err == nil {
			return code, nil
		}

		if params.Redial == 0 || !isDropped(ctx, err, droppedCodes) {
			return 0, fmt.Errorf("failed to carry the terminal: %w", err)
		}

		next, err := redial(ctx, params)
		if err != nil {
			return 0, fmt.Errorf("failed to redial the terminal: %w", err)
		}

		conn.close()
		conn = next
	}
}

type carryParams struct {
	Conn   terminalConn
	Inputs <-chan []byte
	// Unsent is input a lost stream took and never sent, which the next stream sends first.
	Unsent   *[]byte
	Terminal runTerminalParams
}

// carry runs one stream until the terminal exits or the stream fails.
func carry(ctx context.Context, params carryParams) (int, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	forwarded := make(chan struct{})
	go func() {
		defer close(forwarded)
		forward(ctx, params)
	}()

	for {
		message, err := params.Conn.receive()
		if err != nil {
			cancel()
			params.Conn.close()
			<-forwarded

			return 0, fmt.Errorf("failed to receive from the terminal: %w", err)
		}

		if len(message.output) > 0 {
			if _, err := params.Terminal.Out.Write(message.output); err != nil {
				return 0, fmt.Errorf("failed to draw the terminal: %w", err)
			}
		}

		if message.exit != nil {
			return int(message.exit.GetCode()), nil
		}
	}
}

// forward sends what is typed, each resize, and each keepalive or fresh token to one stream, closing
// its input when the typing ends.
func forward(ctx context.Context, params carryParams) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if len(*params.Unsent) > 0 {
		if err := params.Conn.sendInput(*params.Unsent); err != nil {
			return
		}

		*params.Unsent = nil
	}

	interval := params.Terminal.KeepaliveInterval
	if interval == 0 {
		interval = keepaliveInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	tokens := make(chan string)
	if params.Terminal.Token != nil {
		go watchToken(ctx, params.Terminal.Token, interval, tokens)
	}

	inputs := params.Inputs
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := params.Conn.sendKeepalive(); err != nil {
				return
			}
		case token := <-tokens:
			if err := params.Conn.sendToken(token); err != nil {
				return
			}
		case <-params.Terminal.Resized:
			if err := params.Conn.sendResize(params.Terminal.Size()); err != nil {
				return
			}
		case input, ok := <-inputs:
			if !ok {
				_ = params.Conn.closeInput() //nolint:errcheck // a stream that failed is heard about by receive
				inputs = nil

				continue
			}

			if err := params.Conn.sendInput(input); err != nil {
				*params.Unsent = input

				return
			}
		}
	}
}

// watchToken hands on each token from source that differs from the last, asking for one every interval.
func watchToken(ctx context.Context, source func(ctx context.Context) (string, error), interval time.Duration, tokens chan<- string) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	sent := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		token, err := source(ctx)
		if err != nil || token == sent {
			continue
		}

		select {
		case tokens <- token:
			sent = token
		case <-ctx.Done():
			return
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

		if !isDropped(ctx, err, unreachableCodes) || time.Now().After(deadline) {
			return nil, err
		}

		clock.Sleep(ctx, wait)
		if ctx.Err() != nil {
			return nil, fmt.Errorf("failed to dial the engine again: %w", ctx.Err())
		}

		wait = min(wait*2, lastRedialWait)
	}
}

// isDropped reports whether a stream failed with one of codes, or ended without a word.
func isDropped(ctx context.Context, err error, codes []connect.Code) bool {
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

	return slices.Contains(codes, connectErr.Code())
}

func readConsoleSize(fd int) *terminalv1.Size {
	cols, rows, err := term.GetSize(fd)
	if err != nil || rows <= 0 || cols <= 0 {
		return &terminalv1.Size{Rows: 24, Cols: 80}
	}

	return &terminalv1.Size{Rows: uint32(rows), Cols: uint32(cols)} //nolint:gosec // both are positive
}

// awaitSignIn holds a terminal opened before this machine signed in to the engine, saying so once,
// and lets it dial once a sign-in appears.
func awaitSignIn(ctx context.Context, params runTerminalParams) error {
	if params.Token == nil {
		return nil
	}

	_, err := params.Token(ctx)
	if !errors.Is(err, engine.ErrSignInNeeded) {
		return nil
	}

	if _, err := fmt.Fprintf(params.Out, "%v\nThe terminal opens once you sign in.\n", err); err != nil {
		return fmt.Errorf("failed to say the terminal waits on a sign-in: %w", err)
	}

	for errors.Is(err, engine.ErrSignInNeeded) {
		clock.Sleep(ctx, signInCheckInterval)
		if ctx.Err() != nil {
			return fmt.Errorf("failed to wait for a sign-in: %w", ctx.Err())
		}

		_, err = params.Token(ctx)
	}

	return nil
}
