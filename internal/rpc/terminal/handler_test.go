package terminal_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"komrad/internal/attaches"
	"komrad/internal/caller"
	"komrad/internal/interceptors"
	"komrad/internal/pty"
	"komrad/internal/rpc/terminal"
	"komrad/internal/shells"
	"komrad/lib/xhttp"
	terminalv1 "komrad/proto/gen/terminal/v1"
	"komrad/proto/gen/terminal/v1/terminalv1connect"
	terminalv1komrad "komrad/proto/gen/terminal/v1/terminalv1komrad"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

type fakeTerminal struct {
	output  *io.PipeReader
	draw    *io.PipeWriter
	typed   chan []byte
	resized chan pty.Size
	exit    chan int
	closed  chan struct{}
	once    sync.Once
}

func newFakeTerminal() *fakeTerminal {
	output, draw := io.Pipe()

	return &fakeTerminal{
		output:  output,
		draw:    draw,
		typed:   make(chan []byte, 8),
		resized: make(chan pty.Size, 8),
		exit:    make(chan int, 1),
		closed:  make(chan struct{}),
	}
}

func (f *fakeTerminal) Read(p []byte) (int, error) {
	return f.output.Read(p)
}

func (f *fakeTerminal) Write(p []byte) (int, error) {
	f.typed <- append([]byte(nil), p...)

	return len(p), nil
}

func (f *fakeTerminal) Resize(size pty.Size) error {
	f.resized <- size

	return nil
}

func (f *fakeTerminal) Wait() (int, error) {
	return <-f.exit, nil
}

func (f *fakeTerminal) Close() error {
	f.once.Do(func() {
		close(f.closed)
		_ = f.output.Close()
	})

	return nil
}

type fakeOpener struct {
	terminal *fakeTerminal
	opened   chan shells.OpenParams
}

func (f *fakeOpener) Open(_ context.Context, params shells.OpenParams) (pty.Terminal, error) {
	f.opened <- params

	return f.terminal, nil
}

type fakeAttacher struct {
	terminal *fakeTerminal
	opened   chan attaches.OpenParams
}

func (f *fakeAttacher) Open(_ context.Context, params attaches.OpenParams) (pty.Terminal, error) {
	f.opened <- params

	return f.terminal, nil
}

type shellStream = *connect.BidiStreamForClient[terminalv1.ShellRequest, terminalv1.ShellResponse]

type attachStream = *connect.BidiStreamForClient[terminalv1.AttachRequest, terminalv1.AttachResponse]

func serve(t *testing.T, opener *fakeOpener) terminalv1connect.TerminalServiceClient {
	t.Helper()

	return serveBoth(t, opener, &fakeAttacher{})
}

func serveAttaches(t *testing.T, attacher *fakeAttacher) terminalv1connect.TerminalServiceClient {
	t.Helper()

	return serveBoth(t, &fakeOpener{}, attacher)
}

func serveBoth(t *testing.T, opener *fakeOpener, attacher *fakeAttacher) terminalv1connect.TerminalServiceClient {
	t.Helper()

	return startServer(t, newMux(t, terminal.NewHandlerParams{Shells: opener, Attaches: attacher}))
}

func newMux(t *testing.T, params terminal.NewHandlerParams) http.Handler {
	t.Helper()

	validation, err := interceptors.NewValidation()
	require.NoError(t, err)

	params.DrainAfterExit = 50 * time.Millisecond

	mux := http.NewServeMux()
	mux.Handle(terminalv1connect.NewTerminalServiceHandler(
		terminalv1komrad.TerminalServiceAdapter{Impl: terminal.NewHandler(params)},
		connect.WithInterceptors(validation),
	))

	return mux
}

func startServer(t *testing.T, handler http.Handler) terminalv1connect.TerminalServiceClient {
	t.Helper()

	server := httptest.NewUnstartedServer(handler)
	server.Config.Protocols = new(http.Protocols)
	server.Config.Protocols.SetHTTP1(true)
	server.Config.Protocols.SetUnencryptedHTTP2(true)
	server.Start()
	t.Cleanup(server.Close)

	return terminalv1connect.NewTerminalServiceClient(&http.Client{Transport: xhttp.NewStreamTransport()}, server.URL)
}

func openRequest(dir string) *terminalv1.ShellRequest {
	return &terminalv1.ShellRequest{Message: &terminalv1.ShellRequest_Open{Open: &terminalv1.OpenShell{
		Dir:  dir,
		Size: &terminalv1.Size{Rows: 24, Cols: 80},
		Term: "screen-256color",
	}}}
}

func receive(t *testing.T, stream shellStream) *terminalv1.ShellResponse {
	t.Helper()

	response, err := stream.Receive()
	require.NoError(t, err)

	return response
}

// TestShellRefuses covers what the handler answers before a shell is running.
func TestShellRefuses(t *testing.T) {
	tests := []struct {
		name     string
		first    *terminalv1.ShellRequest
		wantCode connect.Code
	}{
		{
			name:     "a stream that does not open first",
			first:    &terminalv1.ShellRequest{Message: &terminalv1.ShellRequest_Input{Input: []byte("ls\n")}},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "a stream that opens with an empty message",
			first:    &terminalv1.ShellRequest{},
			wantCode: connect.CodeInvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := serve(t, &fakeOpener{opened: make(chan shells.OpenParams, 1)})

			stream := client.Shell(t.Context())
			require.NoError(t, stream.Send(tt.first))

			_, err := stream.Receive()
			require.Error(t, err)
			assert.Equal(t, tt.wantCode, connect.CodeOf(err))
		})
	}
}

// TestShellCarriesATerminal covers one shell from open to exit.
func TestShellCarriesATerminal(t *testing.T) {
	fake := newFakeTerminal()
	opener := &fakeOpener{terminal: fake, opened: make(chan shells.OpenParams, 1)}
	client := serve(t, opener)

	stream := client.Shell(t.Context())
	require.NoError(t, stream.Send(openRequest("/work/tree")))

	t.Run("it opens the shell asked for and says it is ready", func(t *testing.T) {
		assert.Equal(t, shells.OpenParams{
			Dir:  "/work/tree",
			Size: pty.Size{Rows: 24, Cols: 80},
			Term: "screen-256color",
		}, <-opener.opened)
		assert.NotNil(t, receive(t, stream).GetReady())
	})

	t.Run("what is typed reaches the shell", func(t *testing.T) {
		require.NoError(t, stream.Send(&terminalv1.ShellRequest{Message: &terminalv1.ShellRequest_Input{Input: []byte("ls\n")}}))
		assert.Equal(t, []byte("ls\n"), <-fake.typed)
	})

	t.Run("a resize reaches the shell", func(t *testing.T) {
		require.NoError(t, stream.Send(&terminalv1.ShellRequest{Message: &terminalv1.ShellRequest_Resize{Resize: &terminalv1.Size{Rows: 30, Cols: 100}}}))
		assert.Equal(t, pty.Size{Rows: 30, Cols: 100}, <-fake.resized)
	})

	t.Run("an empty message types nothing and keeps the shell", func(t *testing.T) {
		require.NoError(t, stream.Send(&terminalv1.ShellRequest{}))
		require.NoError(t, stream.Send(&terminalv1.ShellRequest{Message: &terminalv1.ShellRequest_Input{Input: []byte("pwd\n")}}))
		assert.Equal(t, []byte("pwd\n"), <-fake.typed)
	})

	t.Run("what the shell draws comes back", func(t *testing.T) {
		_, err := fake.draw.Write([]byte("file.go\r\n"))
		require.NoError(t, err)
		assert.Equal(t, []byte("file.go\r\n"), receive(t, stream).GetOutput())
	})

	t.Run("the shell's exit status ends the stream", func(t *testing.T) {
		fake.exit <- 3
		assert.Equal(t, int32(3), receive(t, stream).GetExit().GetCode())

		_, err := stream.Receive()
		assert.ErrorIs(t, err, io.EOF)
	})
}

// TestShellWhenTheCallerStops covers a caller that is done before the shell is.
func TestShellWhenTheCallerStops(t *testing.T) {
	open := func(t *testing.T) (*fakeTerminal, shellStream) {
		t.Helper()

		fake := newFakeTerminal()
		client := serve(t, &fakeOpener{terminal: fake, opened: make(chan shells.OpenParams, 1)})

		stream := client.Shell(t.Context())
		require.NoError(t, stream.Send(openRequest("/work/tree")))
		assert.NotNil(t, receive(t, stream).GetReady())

		return fake, stream
	}

	t.Run("a caller closing its side ends the shell's input and hears it exit", func(t *testing.T) {
		fake, stream := open(t)

		require.NoError(t, stream.CloseRequest())

		select {
		case typed := <-fake.typed:
			assert.Equal(t, []byte{0x04}, typed)
		case <-fake.closed:
			t.Fatal("the shell was hung up rather than told its input ended")
		}

		fake.exit <- 0
		assert.Equal(t, int32(0), receive(t, stream).GetExit().GetCode())
	})

	t.Run("a caller leaving hangs the shell up", func(t *testing.T) {
		fake, stream := open(t)

		require.NoError(t, stream.CloseResponse())

		select {
		case <-fake.closed:
		case <-time.After(5 * time.Second):
			t.Fatal("the shell was not hung up when the caller left")
		}

		fake.exit <- 129
	})
}

func openAttachRequest(agentID string) *terminalv1.AttachRequest {
	return &terminalv1.AttachRequest{Message: &terminalv1.AttachRequest_Open{Open: &terminalv1.OpenAttach{
		AgentId: agentID,
		Size:    &terminalv1.Size{Rows: 24, Cols: 80},
		Term:    "screen-256color",
	}}}
}

func receiveAttach(t *testing.T, stream attachStream) *terminalv1.AttachResponse {
	t.Helper()

	response, err := stream.Receive()
	require.NoError(t, err)

	return response
}

// TestAttachRefuses covers what the handler answers before an attach is running.
func TestAttachRefuses(t *testing.T) {
	tests := []struct {
		name     string
		first    *terminalv1.AttachRequest
		wantCode connect.Code
	}{
		{
			name:     "a stream that does not open first",
			first:    &terminalv1.AttachRequest{Message: &terminalv1.AttachRequest_Input{Input: []byte("ls\n")}},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "a stream that opens with an empty message",
			first:    &terminalv1.AttachRequest{},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "an opening that names no agent",
			first:    openAttachRequest(""),
			wantCode: connect.CodeInvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := serveAttaches(t, &fakeAttacher{opened: make(chan attaches.OpenParams, 1)})

			stream := client.Attach(t.Context())
			require.NoError(t, stream.Send(tt.first))

			_, err := stream.Receive()
			require.Error(t, err)
			assert.Equal(t, tt.wantCode, connect.CodeOf(err))
		})
	}
}

// TestAttachCarriesATerminal covers one attach from open to exit.
func TestAttachCarriesATerminal(t *testing.T) {
	fake := newFakeTerminal()
	attacher := &fakeAttacher{terminal: fake, opened: make(chan attaches.OpenParams, 1)}
	client := serveAttaches(t, attacher)

	stream := client.Attach(t.Context())
	require.NoError(t, stream.Send(openAttachRequest("a1")))

	t.Run("it attaches to the agent asked for and says it is ready", func(t *testing.T) {
		assert.Equal(t, attaches.OpenParams{
			AgentID: "a1",
			Size:    pty.Size{Rows: 24, Cols: 80},
			Term:    "screen-256color",
		}, <-attacher.opened)
		assert.NotNil(t, receiveAttach(t, stream).GetReady())
	})

	t.Run("what is typed reaches the terminal", func(t *testing.T) {
		require.NoError(t, stream.Send(&terminalv1.AttachRequest{Message: &terminalv1.AttachRequest_Input{Input: []byte("ls\n")}}))
		assert.Equal(t, []byte("ls\n"), <-fake.typed)
	})

	t.Run("a resize reaches the terminal", func(t *testing.T) {
		require.NoError(t, stream.Send(&terminalv1.AttachRequest{Message: &terminalv1.AttachRequest_Resize{Resize: &terminalv1.Size{Rows: 30, Cols: 100}}}))
		assert.Equal(t, pty.Size{Rows: 30, Cols: 100}, <-fake.resized)
	})

	t.Run("an empty message types nothing and keeps the attach", func(t *testing.T) {
		require.NoError(t, stream.Send(&terminalv1.AttachRequest{}))
		require.NoError(t, stream.Send(&terminalv1.AttachRequest{Message: &terminalv1.AttachRequest_Input{Input: []byte("pwd\n")}}))
		assert.Equal(t, []byte("pwd\n"), <-fake.typed)
	})

	t.Run("a caller closing its side types nothing and still hears the terminal", func(t *testing.T) {
		require.NoError(t, stream.CloseRequest())

		_, err := fake.draw.Write([]byte("still here\r\n"))
		require.NoError(t, err)
		assert.Equal(t, []byte("still here\r\n"), receiveAttach(t, stream).GetOutput())

		select {
		case typed := <-fake.typed:
			t.Fatalf("the agent's terminal was typed into: %q", typed)
		default:
		}
	})

	t.Run("the attach's exit status ends the stream", func(t *testing.T) {
		fake.exit <- 0
		assert.Equal(t, int32(0), receiveAttach(t, stream).GetExit().GetCode())

		_, err := stream.Receive()
		assert.ErrorIs(t, err, io.EOF)
	})
}

// TestShellRenewsTheCallersSignIn covers a shell held open past the token it opened with.
func TestShellRenewsTheCallersSignIn(t *testing.T) {
	type renewCase struct {
		name      string
		renewErr  error
		wantEnded bool
	}

	tests := []renewCase{
		{
			name:      "a token the sign-in refuses",
			renewErr:  errors.New("another sign-in"),
			wantEnded: true,
		},
		{
			name: "a fresh token for the same caller",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeTerminal()
			renewed := make(chan string, 1)
			mux := newMux(t, terminal.NewHandlerParams{Shells: &fakeOpener{terminal: fake, opened: make(chan shells.OpenParams, 1)}, Attaches: &fakeAttacher{}})
			client := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx := caller.WithRenewal(r.Context(), func(_ context.Context, token string) error {
					renewed <- token

					return tt.renewErr
				})
				mux.ServeHTTP(w, r.WithContext(ctx))
			}))

			stream := client.Shell(t.Context())
			require.NoError(t, stream.Send(openRequest("/work/tree")))
			assert.NotNil(t, receive(t, stream).GetReady())

			require.NoError(t, stream.Send(&terminalv1.ShellRequest{Message: &terminalv1.ShellRequest_Token{Token: "fresh"}}))
			assert.Equal(t, "fresh", <-renewed)

			select {
			case <-fake.closed:
				assert.True(t, tt.wantEnded, "the shell was hung up on a renewed sign-in")
			case <-time.After(200 * time.Millisecond):
				assert.False(t, tt.wantEnded, "the shell outlived a refused sign-in")
			}

			fake.exit <- 0
		})
	}
}

type fakeSSH struct {
	served chan struct{}
}

// Serve echoes the connection back until the caller closes it.
func (f *fakeSSH) Serve(_ context.Context, conn net.Conn) {
	defer close(f.served)

	_, _ = io.Copy(conn, conn)
	_ = conn.Close()
}

func (f *fakeSSH) PublicKey() string {
	return "ssh-ed25519 AAAAkey"
}

func serveSSH(t *testing.T, renew func(token string) error) (terminalv1connect.TerminalServiceClient, *fakeSSH) {
	t.Helper()

	fake := &fakeSSH{served: make(chan struct{})}
	mux := newMux(t, terminal.NewHandlerParams{SSH: fake})

	return startServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := caller.WithRenewal(r.Context(), func(_ context.Context, token string) error {
			return renew(token)
		})
		mux.ServeHTTP(w, r.WithContext(ctx))
	})), fake
}

func sshData(data string) *terminalv1.SSHRequest {
	return &terminalv1.SSHRequest{Message: &terminalv1.SSHRequest_Data{Data: []byte(data)}}
}

// TestSSHCarriesAConnection covers one SSH connection from the first bytes to the caller leaving.
func TestSSHCarriesAConnection(t *testing.T) {
	renewed := make(chan string, 1)
	client, fake := serveSSH(t, func(token string) error {
		renewed <- token

		return nil
	})

	stream := client.SSH(t.Context())

	t.Run("bytes reach the server and its answer comes back", func(t *testing.T) {
		require.NoError(t, stream.Send(sshData("SSH-2.0-client\r\n")))

		response, err := stream.Receive()
		require.NoError(t, err)
		assert.Equal(t, []byte("SSH-2.0-client\r\n"), response.GetData())
	})

	t.Run("an empty message carries nothing", func(t *testing.T) {
		require.NoError(t, stream.Send(&terminalv1.SSHRequest{}))
		require.NoError(t, stream.Send(sshData("after")))

		response, err := stream.Receive()
		require.NoError(t, err)
		assert.Equal(t, []byte("after"), response.GetData())
	})

	t.Run("a token renews the caller's sign-in", func(t *testing.T) {
		require.NoError(t, stream.Send(&terminalv1.SSHRequest{Message: &terminalv1.SSHRequest_Token{Token: "fresh"}}))
		assert.Equal(t, "fresh", <-renewed)
	})

	t.Run("the caller closing its side ends the connection", func(t *testing.T) {
		require.NoError(t, stream.CloseRequest())
		<-fake.served

		_, err := stream.Receive()
		assert.ErrorIs(t, err, io.EOF)
	})
}

// TestSSHEndsOnARefusedSignIn covers a token the sign-in will not take.
func TestSSHEndsOnARefusedSignIn(t *testing.T) {
	client, fake := serveSSH(t, func(string) error {
		return errors.New("another sign-in")
	})

	stream := client.SSH(t.Context())
	require.NoError(t, stream.Send(&terminalv1.SSHRequest{Message: &terminalv1.SSHRequest_Token{Token: "someone else"}}))

	select {
	case <-fake.served:
	case <-time.After(5 * time.Second):
		t.Fatal("the connection outlived a refused sign-in")
	}
}

// TestGetSSHHostKey covers the key a client trusts before it connects.
func TestGetSSHHostKey(t *testing.T) {
	client, _ := serveSSH(t, func(string) error { return nil })

	res, err := client.GetSSHHostKey(t.Context(), connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)
	assert.Equal(t, "ssh-ed25519 AAAAkey", res.Msg.GetPublicKey())
}
