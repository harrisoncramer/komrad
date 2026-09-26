package cli

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"syscall"
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

type fakeTerminalService struct {
	terminalv1connect.UnimplementedTerminalServiceHandler

	refuse   error
	received chan *terminalv1.ShellRequest
	respond  chan *terminalv1.ShellResponse
	ended    chan struct{}
	left     chan struct{}
}

func (f *fakeTerminalService) Shell(ctx context.Context, stream *connect.BidiStream[terminalv1.ShellRequest, terminalv1.ShellResponse]) error {
	open, err := stream.Receive()
	if err != nil {
		return err
	}
	f.received <- open

	if f.refuse != nil {
		return f.refuse
	}

	go func() {
		for {
			request, err := stream.Receive()
			if errors.Is(err, io.EOF) && f.ended != nil {
				close(f.ended)
			}

			if err != nil {
				return
			}
			f.received <- request
		}
	}()

	for {
		select {
		case <-ctx.Done():
			if f.left != nil {
				close(f.left)
			}

			return nil
		case response := <-f.respond:
			if err := stream.Send(response); err != nil {
				return err
			}

			if response.GetExit() != nil {
				return nil
			}
		}
	}
}

func serveTerminals(t *testing.T, service *fakeTerminalService) terminalv1connect.TerminalServiceClient {
	t.Helper()

	mux := http.NewServeMux()
	mux.Handle(terminalv1connect.NewTerminalServiceHandler(service))

	server := httptest.NewUnstartedServer(mux)
	server.Config.Protocols = xhttp.NewServerProtocols()
	server.Start()
	t.Cleanup(server.Close)

	return terminalv1connect.NewTerminalServiceClient(xhttp.NewClientOver(xhttp.NewStreamTransport(), 0), server.URL)
}

type shellRun struct {
	code int
	err  error
}

// TestRunShellRefusesWithTheEnginesReason covers a shell the engine will not open.
func TestRunShellRefusesWithTheEnginesReason(t *testing.T) {
	service := &fakeTerminalService{
		refuse:   connect.NewError(connect.CodePermissionDenied, os.ErrPermission),
		received: make(chan *terminalv1.ShellRequest, 8),
	}
	readied := false

	_, err := runShell(t.Context(), runShellParams{
		Client: serveTerminals(t, service),
		Dir:    "/etc",
		In:     eofReader{},
		Out:    io.Discard,
		Size: func() *terminalv1.Size {
			return &terminalv1.Size{Rows: 24, Cols: 80}
		},
		Resized: make(chan os.Signal),
		Ready: func() (func(), error) {
			readied = true

			return func() {}, nil
		},
	})

	require.Error(t, err)
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	assert.False(t, readied, "the terminal was put in raw mode for a shell that never opened")
}

// TestRunShellCarriesTheConsole covers one shell from open to exit on the client side.
func TestRunShellCarriesTheConsole(t *testing.T) {
	service := &fakeTerminalService{
		received: make(chan *terminalv1.ShellRequest, 8),
		respond:  make(chan *terminalv1.ShellResponse, 8),
	}
	typed, typing := io.Pipe()
	t.Cleanup(func() { _ = typing.Close() })
	resized := make(chan os.Signal, 1)
	var sizing sync.Mutex
	size := &terminalv1.Size{Rows: 24, Cols: 80}
	var out syncBuffer
	restored := make(chan struct{})

	service.respond <- &terminalv1.ShellResponse{Message: &terminalv1.ShellResponse_Ready{Ready: &emptypb.Empty{}}}

	done := make(chan shellRun, 1)
	go func() {
		code, err := runShell(t.Context(), runShellParams{
			Client: serveTerminals(t, service),
			Dir:    "/work/tree",
			Term:   "screen-256color",
			In:     typed,
			Out:    &out,
			Size: func() *terminalv1.Size {
				sizing.Lock()
				defer sizing.Unlock()

				return size
			},
			Resized: resized,
			Ready: func() (func(), error) {
				return func() { close(restored) }, nil
			},
		})
		done <- shellRun{code: code, err: err}
	}()

	t.Run("it opens the shell with this terminal's directory, size and type", func(t *testing.T) {
		open := (<-service.received).GetOpen()
		require.NotNil(t, open)
		assert.Equal(t, "/work/tree", open.GetDir())
		assert.Equal(t, uint32(24), open.GetSize().GetRows())
		assert.Equal(t, uint32(80), open.GetSize().GetCols())
		assert.Equal(t, "screen-256color", open.GetTerm())
	})

	t.Run("what is typed is sent", func(t *testing.T) {
		_, err := typing.Write([]byte("ls\n"))
		require.NoError(t, err)
		assert.Equal(t, []byte("ls\n"), (<-service.received).GetInput())
	})

	t.Run("a resize sends the size the terminal has now", func(t *testing.T) {
		sizing.Lock()
		size = &terminalv1.Size{Rows: 30, Cols: 100}
		sizing.Unlock()

		resized <- syscall.SIGWINCH
		resize := (<-service.received).GetResize()
		assert.Equal(t, uint32(30), resize.GetRows())
		assert.Equal(t, uint32(100), resize.GetCols())
	})

	t.Run("the shell's exit status is the answer and the terminal is given back", func(t *testing.T) {
		service.respond <- &terminalv1.ShellResponse{Message: &terminalv1.ShellResponse_Output{Output: []byte("file.go\r\n")}}
		service.respond <- &terminalv1.ShellResponse{Message: &terminalv1.ShellResponse_Exit{Exit: &terminalv1.Exit{Code: 3}}}

		run := <-done
		require.NoError(t, run.err)
		assert.Equal(t, 3, run.code)
		assert.Equal(t, "file.go\r\n", out.String())
		<-restored
	})
}

// TestRunShellWhenThisSideStops covers the client finishing before the shell does.
func TestRunShellWhenThisSideStops(t *testing.T) {
	start := func(ctx context.Context, t *testing.T, service *fakeTerminalService, in io.Reader) chan shellRun {
		t.Helper()

		service.respond <- &terminalv1.ShellResponse{Message: &terminalv1.ShellResponse_Ready{Ready: &emptypb.Empty{}}}

		done := make(chan shellRun, 1)
		go func() {
			code, err := runShell(ctx, runShellParams{
				Client: serveTerminals(t, service),
				Dir:    "/work/tree",
				In:     in,
				Out:    io.Discard,
				Size: func() *terminalv1.Size {
					return &terminalv1.Size{Rows: 24, Cols: 80}
				},
				Resized: make(chan os.Signal),
				Ready: func() (func(), error) {
					return func() {}, nil
				},
			})
			done <- shellRun{code: code, err: err}
		}()

		return done
	}

	t.Run("input that ends closes this side and the exit status still comes back", func(t *testing.T) {
		service := &fakeTerminalService{
			received: make(chan *terminalv1.ShellRequest, 8),
			respond:  make(chan *terminalv1.ShellResponse, 8),
			ended:    make(chan struct{}),
		}
		done := start(t.Context(), t, service, strings.NewReader("ls\n"))

		assert.NotNil(t, (<-service.received).GetOpen())
		assert.Equal(t, []byte("ls\n"), (<-service.received).GetInput())

		select {
		case <-service.ended:
		case <-time.After(5 * time.Second):
			t.Fatal("the engine was not told the input ended")
		}

		service.respond <- &terminalv1.ShellResponse{Message: &terminalv1.ShellResponse_Exit{Exit: &terminalv1.Exit{Code: 2}}}

		run := <-done
		require.NoError(t, run.err)
		assert.Equal(t, 2, run.code)
	})

	t.Run("a cancelled caller hangs the shell up", func(t *testing.T) {
		service := &fakeTerminalService{
			received: make(chan *terminalv1.ShellRequest, 8),
			respond:  make(chan *terminalv1.ShellResponse, 8),
			left:     make(chan struct{}),
		}
		typed, typing := io.Pipe()
		t.Cleanup(func() { _ = typing.Close() })

		ctx, cancel := context.WithCancel(t.Context())
		done := start(ctx, t, service, typed)
		assert.NotNil(t, (<-service.received).GetOpen())

		cancel()

		select {
		case <-service.left:
		case <-time.After(5 * time.Second):
			t.Fatal("the engine did not hear the caller leave")
		}

		assert.Error(t, (<-done).err)
	})
}

type eofReader struct{}

func (eofReader) Read([]byte) (int, error) {
	return 0, io.EOF
}

type syncBuffer struct {
	mutex sync.Mutex
	data  []byte
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	b.data = append(b.data, p...)

	return len(p), nil
}

func (b *syncBuffer) String() string {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	return string(b.data)
}
