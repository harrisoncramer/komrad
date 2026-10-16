package cli

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"komrad/lib/xhttp"
	terminalv1 "komrad/proto/gen/terminal/v1"
	"komrad/proto/gen/terminal/v1/terminalv1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

type fakeSSHService struct {
	terminalv1connect.UnimplementedTerminalServiceHandler
	received chan *terminalv1.SSHRequest
}

// SSH echoes each message's bytes back and records every message.
func (f *fakeSSHService) SSH(_ context.Context, stream *connect.BidiStream[terminalv1.SSHRequest, terminalv1.SSHResponse]) error {
	for {
		request, err := stream.Receive()
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return err
		}

		f.received <- request

		if data := request.GetData(); len(data) > 0 {
			if err := stream.Send(&terminalv1.SSHResponse{Data: data}); err != nil {
				return err
			}
		}
	}
}

func (f *fakeSSHService) GetSSHHostKey(context.Context, *connect.Request[emptypb.Empty]) (*connect.Response[terminalv1.GetSSHHostKeyResponse], error) {
	return connect.NewResponse(&terminalv1.GetSSHHostKeyResponse{PublicKey: "ssh-ed25519 AAAAkey"}), nil
}

func serveSSH(t *testing.T, service *fakeSSHService) terminalv1connect.TerminalServiceClient {
	t.Helper()

	mux := http.NewServeMux()
	mux.Handle(terminalv1connect.NewTerminalServiceHandler(service))

	server := httptest.NewUnstartedServer(mux)
	server.Config.Protocols = xhttp.NewServerProtocols()
	server.Start()
	t.Cleanup(server.Close)

	return terminalv1connect.NewTerminalServiceClient(xhttp.NewClientOver(xhttp.NewStreamTransport(), 0), server.URL)
}

// nextWithData skips the empty messages that keep the stream open.
func nextWithData(t *testing.T, received <-chan *terminalv1.SSHRequest) *terminalv1.SSHRequest {
	t.Helper()

	for request := range received {
		if request.GetMessage() != nil {
			return request
		}
	}

	t.Fatal("the stream ended")

	return nil
}

// TestRunSSHProxy covers one connection carried between ssh and the engine.
func TestRunSSHProxy(t *testing.T) {
	service := &fakeSSHService{received: make(chan *terminalv1.SSHRequest, 64)}
	in, typed := io.Pipe()
	shown, out := io.Pipe()

	ran := make(chan error, 1)
	go func() {
		ran <- runSSHProxy(t.Context(), runSSHProxyParams{
			Client:    serveSSH(t, service),
			Token:     func(context.Context) (string, error) { return "fresh", nil },
			In:        in,
			Out:       out,
			Keepalive: 10 * time.Millisecond,
		})
	}()

	t.Run("the stream opens before ssh says anything", func(t *testing.T) {
		assert.Nil(t, (<-service.received).GetMessage())
	})

	t.Run("a fresh token goes up once", func(t *testing.T) {
		assert.Equal(t, "fresh", nextWithData(t, service.received).GetToken())
	})

	t.Run("what ssh writes reaches the engine and its answer comes back", func(t *testing.T) {
		_, err := typed.Write([]byte("SSH-2.0-client\r\n"))
		require.NoError(t, err)
		assert.Equal(t, []byte("SSH-2.0-client\r\n"), nextWithData(t, service.received).GetData())

		answer := make([]byte, len("SSH-2.0-client\r\n"))
		_, err = io.ReadFull(shown, answer)
		require.NoError(t, err)
		assert.Equal(t, "SSH-2.0-client\r\n", string(answer))
	})

	t.Run("a quiet stream keeps sending empty messages", func(t *testing.T) {
		for range 3 {
			assert.Nil(t, (<-service.received).GetMessage())
		}
	})

	t.Run("ssh closing its side ends the connection", func(t *testing.T) {
		require.NoError(t, typed.Close())

		select {
		case err := <-ran:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("the proxy outlived ssh")
		}
	})
}

// TestWriteSSHHost covers the ssh host komrad login writes for an engine.
func TestWriteSSHHost(t *testing.T) {
	dir := t.TempDir()
	host := sshHost{
		Name:       "komrad-remote",
		Config:     filepath.Join(dir, "komrad-remote.conf"),
		KnownHosts: filepath.Join(dir, "komrad-remote.known_hosts"),
	}

	require.NoError(t, writeSSHHost(t.Context(), writeSSHHostParams{
		Client: serveSSH(t, &fakeSSHService{}),
		Host:   host,
		Proxy:  []string{"/opt/my apps/komrad", "--engine", "remote"},
	}))

	config, err := os.ReadFile(host.Config)
	require.NoError(t, err)
	assert.Equal(t, "Host komrad-remote\n"+
		"  ProxyCommand '/opt/my apps/komrad' '--engine' 'remote' ssh-proxy\n"+
		"  HostKeyAlias komrad-remote\n"+
		"  UserKnownHostsFile \""+host.KnownHosts+"\"\n"+
		"  StrictHostKeyChecking yes\n", string(config))

	knownHosts, err := os.ReadFile(host.KnownHosts)
	require.NoError(t, err)
	assert.Equal(t, "komrad-remote ssh-ed25519 AAAAkey\n", string(knownHosts))
}
