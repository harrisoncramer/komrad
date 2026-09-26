package terminal_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

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
	err      error
	opened   chan shells.OpenParams
}

func (f *fakeOpener) Open(_ context.Context, params shells.OpenParams) (pty.Terminal, error) {
	if f.err != nil {
		return nil, f.err
	}

	f.opened <- params

	return f.terminal, nil
}

type shellStream = *connect.BidiStreamForClient[terminalv1.ShellRequest, terminalv1.ShellResponse]

func serve(t *testing.T, opener *fakeOpener) terminalv1connect.TerminalServiceClient {
	t.Helper()

	validation, err := interceptors.NewValidation()
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.Handle(terminalv1connect.NewTerminalServiceHandler(
		terminalv1komrad.TerminalServiceAdapter{Impl: terminal.NewHandler(terminal.NewHandlerParams{
			Shells:         opener,
			DrainAfterExit: 50 * time.Millisecond,
		})},
		connect.WithInterceptors(validation),
	))

	server := httptest.NewUnstartedServer(mux)
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
		openErr  error
		wantCode connect.Code
	}{
		{
			name:     "a stream that does not open first",
			first:    &terminalv1.ShellRequest{Message: &terminalv1.ShellRequest_Input{Input: []byte("ls\n")}},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "a directory outside every live agent",
			first:    openRequest("/etc"),
			openErr:  shells.ErrOutsideAgents,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name:     "a directory that does not exist",
			first:    openRequest("/nowhere"),
			openErr:  shells.ErrNoDirectory,
			wantCode: connect.CodeInvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := serve(t, &fakeOpener{err: tt.openErr, opened: make(chan shells.OpenParams, 1)})

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
