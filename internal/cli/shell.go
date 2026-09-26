package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"komrad/lib/xhttp"
	terminalv1 "komrad/proto/gen/terminal/v1"
	"komrad/proto/gen/terminal/v1/terminalv1connect"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var errShellEndedEarly = errors.New("the shell ended without an exit status")

// ExitError is a command whose answer is an exit status rather than a message.
type ExitError struct {
	Code int
}

// Error says which status the command exited with.
func (e *ExitError) Error() string {
	return fmt.Sprintf("exit status %d", e.Code)
}

func shellCmd(state *app) *cobra.Command {
	var addr string

	cmd := &cobra.Command{
		Use:   "shell DIR",
		Short: "Open a shell on the engine in a directory an agent works in",
		Long: `Open a shell on the engine in a directory an agent works in.

The shell runs on the engine and is carried to this terminal over the engine's
api, so it works the same whether the engine is this machine or another one. The
directory has to be inside a live agent's directory. The shell's exit status is
this command's.`,
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

			code, err := runConsoleShell(ctx, runConsoleShellParams{
				Client: terminalv1connect.NewTerminalServiceClient(xhttp.NewClientOver(xhttp.NewStreamTransport(), 0), "http://"+addr),
				Dir:    args[0],
				Term:   state.env.Term,
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

type runConsoleShellParams struct {
	Client terminalv1connect.TerminalServiceClient
	Dir    string
	Term   string
}

// runConsoleShell runs a shell on this process's own terminal, raw while it runs and resized with it.
func runConsoleShell(ctx context.Context, params runConsoleShellParams) (int, error) {
	fd := int(os.Stdin.Fd()) //nolint:gosec // a descriptor fits an int
	isConsole := term.IsTerminal(fd)

	resized := make(chan os.Signal, 1)
	signal.Notify(resized, syscall.SIGWINCH)
	defer signal.Stop(resized)

	return runShell(ctx, runShellParams{
		Client:  params.Client,
		Dir:     params.Dir,
		Term:    params.Term,
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

type runShellParams struct {
	Client  terminalv1connect.TerminalServiceClient
	Dir     string
	Term    string
	In      io.Reader
	Out     io.Writer
	Size    func() *terminalv1.Size
	Resized <-chan os.Signal
	Ready   func() (func(), error)
}

// runShell opens a shell and carries In to it and its output to Out until it exits.
func runShell(ctx context.Context, params runShellParams) (int, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stream := params.Client.Shell(ctx)
	defer stream.CloseResponse() //nolint:errcheck // the shell has ended

	var sending sync.Mutex
	send := func(request *terminalv1.ShellRequest) error {
		sending.Lock()
		defer sending.Unlock()

		return stream.Send(request)
	}
	closeRequest := func() error {
		sending.Lock()
		defer sending.Unlock()

		return stream.CloseRequest()
	}

	open := &terminalv1.ShellRequest{Message: &terminalv1.ShellRequest_Open{Open: &terminalv1.OpenShell{
		Dir:  params.Dir,
		Size: params.Size(),
		Term: params.Term,
	}}}
	if err := send(open); err != nil && !errors.Is(err, io.EOF) {
		return 0, fmt.Errorf("failed to open the shell: %w", err)
	}

	first, err := stream.Receive()
	if err != nil {
		return 0, fmt.Errorf("failed to open a shell in %s: %w", params.Dir, err)
	}

	if first.GetReady() == nil {
		return 0, fmt.Errorf("failed to open a shell in %s: the engine did not say it was ready", params.Dir)
	}

	restore, err := params.Ready()
	if err != nil {
		return 0, err
	}
	defer restore()

	go forwardResizes(ctx, params, send)
	go forwardInput(params.In, send, closeRequest)

	for {
		response, err := stream.Receive()
		if errors.Is(err, io.EOF) {
			return 0, errShellEndedEarly
		}

		if err != nil {
			return 0, fmt.Errorf("failed to hear from the shell in %s: %w", params.Dir, err)
		}

		switch message := response.GetMessage().(type) {
		case *terminalv1.ShellResponse_Output:
			if _, err := params.Out.Write(message.Output); err != nil {
				return 0, fmt.Errorf("failed to draw the shell: %w", err)
			}
		case *terminalv1.ShellResponse_Exit:
			return int(message.Exit.GetCode()), nil
		case *terminalv1.ShellResponse_Ready:
		}
	}
}

func forwardResizes(ctx context.Context, params runShellParams, send func(*terminalv1.ShellRequest) error) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-params.Resized:
			resize := &terminalv1.ShellRequest{Message: &terminalv1.ShellRequest_Resize{Resize: params.Size()}}
			if err := send(resize); err != nil {
				return
			}
		}
	}
}

// forwardInput sends what is read from in, closing this side of the stream when in ends.
func forwardInput(in io.Reader, send func(*terminalv1.ShellRequest) error, closeRequest func() error) {
	buffer := make([]byte, 32*1024)
	for {
		n, err := in.Read(buffer)
		if n > 0 {
			input := &terminalv1.ShellRequest{Message: &terminalv1.ShellRequest_Input{Input: append([]byte(nil), buffer[:n]...)}}
			if sendErr := send(input); sendErr != nil {
				return
			}
		}

		if errors.Is(err, io.EOF) {
			_ = closeRequest() //nolint:errcheck // a stream that failed is heard about by Receive

			return
		}

		if err != nil {
			return
		}
	}
}

func readConsoleSize(fd int) *terminalv1.Size {
	cols, rows, err := term.GetSize(fd)
	if err != nil || rows <= 0 || cols <= 0 {
		return &terminalv1.Size{Rows: 24, Cols: 80}
	}

	return &terminalv1.Size{Rows: uint32(rows), Cols: uint32(cols)} //nolint:gosec // both are positive
}
