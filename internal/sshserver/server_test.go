package sshserver_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"komrad/internal/sshserver"

	"github.com/pkg/sftp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"
)

func newServer(t *testing.T, stateDir string) *sshserver.Server {
	t.Helper()

	server, err := sshserver.NewServer(sshserver.NewServerParams{
		Logger:   slog.New(slog.DiscardHandler),
		StateDir: stateDir,
		Shell:    "/bin/sh",
	})
	require.NoError(t, err)

	return server
}

// connPair is two ends of a loopback connection, which buffers where net.Pipe would leave both
// sides of a handshake waiting on each other.
func connPair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, _ := listener.Accept()
		accepted <- conn
	}()

	clientSide, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)

	return clientSide, <-accepted
}

func connect(t *testing.T, server *sshserver.Server) *gossh.Client {
	t.Helper()

	clientSide, serverSide := connPair(t)
	go server.Serve(t.Context(), serverSide)

	hostKey, _, _, _, err := gossh.ParseAuthorizedKey([]byte(server.PublicKey())) //nolint:dogsled // only the key matters
	require.NoError(t, err)

	conn, channels, requests, err := gossh.NewClientConn(clientSide, "komrad", &gossh.ClientConfig{
		User:            "anyone",
		HostKeyCallback: gossh.FixedHostKey(hostKey),
	})
	require.NoError(t, err)

	client := gossh.NewClient(conn, channels, requests)
	t.Cleanup(func() { _ = client.Close() })

	return client
}

// TestSessions covers the commands a client runs.
func TestSessions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	client := connect(t, newServer(t, t.TempDir()))

	tests := []struct {
		name       string
		command    string
		stdin      string
		terminal   bool
		wantOutput string
		wantCode   int
	}{
		{
			name:       "a command runs in the home directory",
			command:    "pwd",
			wantOutput: home,
		},
		{
			name:     "a command's exit status comes back",
			command:  "exit 3",
			wantCode: 3,
		},
		{
			name:       "what the client sends is the command's input",
			command:    "read line; echo got-$line",
			stdin:      "hello\n",
			wantOutput: "got-hello",
		},
		{
			name:       "a command that ignores its input still ends",
			command:    "echo done",
			wantOutput: "done",
		},
		{
			name:       "a command on a terminal draws on the size and type the client asked for",
			command:    "echo $TERM; stty size",
			terminal:   true,
			wantOutput: "screen-256color\r\n30 100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session, err := client.NewSession()
			require.NoError(t, err)

			if tt.terminal {
				require.NoError(t, session.RequestPty("screen-256color", 30, 100, gossh.TerminalModes{}))
			}

			var output bytes.Buffer
			session.Stdout = &output
			session.Stdin = strings.NewReader(tt.stdin)

			err = session.Run(tt.command)

			code := 0
			var exited *gossh.ExitError
			if err != nil {
				require.ErrorAs(t, err, &exited)
				code = exited.ExitStatus()
			}

			assert.Equal(t, tt.wantCode, code)
			assert.Contains(t, output.String(), tt.wantOutput)
		})
	}
}

// TestFiles covers moving a file over sftp, the way scp and editors do.
func TestFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	client, err := sftp.NewClient(connect(t, newServer(t, t.TempDir())))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	file, err := client.Create("notes.txt")
	require.NoError(t, err)
	_, err = file.Write([]byte("edited"))
	require.NoError(t, err)
	require.NoError(t, file.Close())

	written, err := os.ReadFile(filepath.Join(home, "notes.txt"))
	require.NoError(t, err)
	assert.Equal(t, "edited", string(written))
}

// TestForwarding covers a client reaching a port and a socket on the engine.
func TestForwarding(t *testing.T) {
	client := connect(t, newServer(t, t.TempDir()))

	socketDir, err := os.MkdirTemp("/tmp", "komrad-ssh") //nolint:usetesting // a socket path under the test directory runs past the length unix allows
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })

	tests := []struct {
		name    string
		network string
		address string
	}{
		{name: "a port", network: "tcp", address: "127.0.0.1:0"},
		{name: "a unix socket", network: "unix", address: filepath.Join(socketDir, "s")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listener, err := net.Listen(tt.network, tt.address)
			require.NoError(t, err)
			t.Cleanup(func() { _ = listener.Close() })

			go func() {
				accepted, err := listener.Accept()
				if err != nil {
					return
				}
				defer accepted.Close()

				_, _ = io.Copy(accepted, accepted)
			}()

			forwarded, err := client.Dial(tt.network, listener.Addr().String())
			require.NoError(t, err)
			defer forwarded.Close()

			_, err = forwarded.Write([]byte("ping"))
			require.NoError(t, err)

			echoed := make([]byte, 4)
			_, err = io.ReadFull(forwarded, echoed)
			require.NoError(t, err)
			assert.Equal(t, "ping", string(echoed))
		})
	}
}

// TestHostKey covers the key the server proves itself with.
func TestHostKey(t *testing.T) {
	stateDir := t.TempDir()
	first := newServer(t, stateDir)

	assert.True(t, strings.HasPrefix(first.PublicKey(), "ssh-ed25519 "))
	assert.Equal(t, first.PublicKey(), newServer(t, stateDir).PublicKey(), "a restart made a new key")
	assert.NotEqual(t, first.PublicKey(), newServer(t, t.TempDir()).PublicKey(), "two engines share a key")
}

// TestServeEndsWithItsContext covers a caller whose sign-in ends while connected.
func TestServeEndsWithItsContext(t *testing.T) {
	server := newServer(t, t.TempDir())
	clientSide, serverSide := connPair(t)
	t.Cleanup(func() { _ = clientSide.Close() })

	ctx, cancel := context.WithCancel(t.Context())
	served := make(chan struct{})
	go func() {
		defer close(served)
		server.Serve(ctx, serverSide)
	}()

	cancel()

	select {
	case <-served:
	case <-time.After(5 * time.Second):
		t.Fatal("the connection outlived its context")
	}
}
