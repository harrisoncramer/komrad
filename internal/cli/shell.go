package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"komrad/lib/xhttp"
	terminalv1 "komrad/proto/gen/terminal/v1"
	"komrad/proto/gen/terminal/v1/terminalv1connect"

	"github.com/spf13/cobra"
)

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

			client := terminalv1connect.NewTerminalServiceClient(xhttp.NewClientOver(xhttp.NewStreamTransport(), 0), "http://"+addr)
			code, err := runConsole(ctx, runConsoleParams{
				Dial: dialShell(client, args[0], state.env.Term),
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
	return runTerminal(ctx, runTerminalParams{
		Dial:    dialShell(params.Client, params.Dir, params.Term),
		In:      params.In,
		Out:     params.Out,
		Size:    params.Size,
		Resized: params.Resized,
		Ready:   params.Ready,
	})
}

func dialShell(client terminalv1connect.TerminalServiceClient, dir, term string) dialFunc {
	return func(ctx context.Context, size *terminalv1.Size) (terminalConn, error) {
		conn := &streamConn[terminalv1.ShellRequest, terminalv1.ShellResponse]{
			stream: client.Shell(ctx),
			input: func(input []byte) *terminalv1.ShellRequest {
				return &terminalv1.ShellRequest{Message: &terminalv1.ShellRequest_Input{Input: input}}
			},
			resize: func(size *terminalv1.Size) *terminalv1.ShellRequest {
				return &terminalv1.ShellRequest{Message: &terminalv1.ShellRequest_Resize{Resize: size}}
			},
			read: func(response *terminalv1.ShellResponse) received {
				return received{
					ready:  response.GetReady() != nil,
					output: response.GetOutput(),
					exit:   response.GetExit(),
				}
			},
		}

		open := &terminalv1.ShellRequest{Message: &terminalv1.ShellRequest_Open{Open: &terminalv1.OpenShell{
			Dir:  dir,
			Size: size,
			Term: term,
		}}}
		if err := conn.open(open); err != nil {
			return nil, fmt.Errorf("failed to open a shell in %s: %w", dir, err)
		}

		return conn, nil
	}
}
