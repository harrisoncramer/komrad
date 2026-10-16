package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"komrad/lib/xhttp"
	terminalv1 "komrad/proto/gen/terminal/v1"
	"komrad/proto/gen/terminal/v1/terminalv1connect"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
)

// sshKeepalive is how often a quiet SSH stream sends a message, well inside the minute a proxy in
// front of the engine lets a request go silent.
const sshKeepalive = 20 * time.Second

func sshProxyCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "ssh-proxy",
		Short: "Carry an SSH connection to the engine over its api",
		Long: `Carry an SSH connection to the engine over its api, between ssh and the engine's SSH server.

ssh runs this as the ProxyCommand of the host komrad login writes, and it reads and writes the
connection on its standard input and output. It sends the engine each fresh sign-in, and a message
every 20 seconds so a quiet connection stays open.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if state.env.URL == "" {
				return errNoEngineURL
			}

			client := terminalv1connect.NewTerminalServiceClient(xhttp.NewClientOver(state.signTransport(xhttp.NewStreamTransport()), 0), state.getAPIBase())

			return runSSHProxy(cmd.Context(), runSSHProxyParams{
				Client: client,
				Token:  state.getTokenSource(),
				In:     os.Stdin,
				Out:    os.Stdout,
			})
		},
	}
}

type sshProxy struct {
	stream  *connect.BidiStreamForClient[terminalv1.SSHRequest, terminalv1.SSHResponse]
	sending sync.Mutex
}

type runSSHProxyParams struct {
	Client terminalv1connect.TerminalServiceClient
	// Token is a fresh token for the engine, or nil when the engine takes none.
	Token     func(ctx context.Context) (string, error)
	In        io.Reader
	Out       io.Writer
	Keepalive time.Duration
}

// runSSHProxy carries In to the engine's SSH server and what it sends back to Out, until either
// side closes.
func runSSHProxy(ctx context.Context, params runSSHProxyParams) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	proxy := &sshProxy{stream: params.Client.SSH(ctx)}
	defer func() { _ = proxy.stream.CloseResponse() }() //nolint:errcheck // the stream is being let go

	if err := proxy.send(&terminalv1.SSHRequest{}); err != nil && !errors.Is(err, io.EOF) {
		return err
	}

	go proxy.sendInput(params.In)
	go proxy.keepAlive(ctx, params)

	for {
		response, err := proxy.stream.Receive()
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return fmt.Errorf("failed to read the SSH connection from the engine: %w", err)
		}

		if _, err := params.Out.Write(response.GetData()); err != nil {
			return fmt.Errorf("failed to hand the SSH connection to ssh: %w", err)
		}
	}
}

func (p *sshProxy) send(request *terminalv1.SSHRequest) error {
	p.sending.Lock()
	defer p.sending.Unlock()

	if err := p.stream.Send(request); err != nil {
		return fmt.Errorf("failed to send the SSH connection to the engine: %w", err)
	}

	return nil
}

func (p *sshProxy) sendInput(in io.Reader) {
	buffer := make([]byte, 32*1024)
	for {
		n, err := in.Read(buffer)
		if n > 0 {
			if sendErr := p.send(&terminalv1.SSHRequest{Message: &terminalv1.SSHRequest_Data{Data: append([]byte(nil), buffer[:n]...)}}); sendErr != nil {
				return
			}
		}

		if err != nil {
			p.sending.Lock()
			_ = p.stream.CloseRequest() //nolint:errcheck // a stream that failed is heard about by Receive
			p.sending.Unlock()

			return
		}
	}
}

// keepAlive sends each fresh token, and an empty message when there is none, every keepalive.
func (p *sshProxy) keepAlive(ctx context.Context, params runSSHProxyParams) {
	every := params.Keepalive
	if every == 0 {
		every = sshKeepalive
	}

	ticker := time.NewTicker(every)
	defer ticker.Stop()

	sent := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		request := &terminalv1.SSHRequest{}
		if params.Token != nil {
			if token, err := params.Token(ctx); err == nil && token != sent {
				request.Message = &terminalv1.SSHRequest_Token{Token: token}
				sent = token
			}
		}

		if err := p.send(request); err != nil {
			return
		}
	}
}
