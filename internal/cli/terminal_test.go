package cli

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"komrad/internal/engine"
	"komrad/lib/xhttp"
	terminalv1 "komrad/proto/gen/terminal/v1"
	"komrad/proto/gen/terminal/v1/terminalv1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

type attachStep struct {
	refuse     error
	output     string
	exit       bool
	reset      bool
	disconnect bool
	listen     bool
}

type fakeAttachService struct {
	terminalv1connect.UnimplementedTerminalServiceHandler
	mu               sync.Mutex
	steps            []attachStep
	opened           []*terminalv1.OpenAttach
	heard            chan *terminalv1.AttachRequest
	closeConnections func()
}

func (f *fakeAttachService) Attach(_ context.Context, stream *connect.BidiStream[terminalv1.AttachRequest, terminalv1.AttachResponse]) error {
	open, err := stream.Receive()
	if err != nil {
		return err
	}

	f.mu.Lock()
	f.opened = append(f.opened, open.GetOpen())
	step := f.steps[0]
	if len(f.steps) > 1 {
		f.steps = f.steps[1:]
	}
	f.mu.Unlock()

	if step.refuse != nil {
		return step.refuse
	}

	if err := stream.Send(&terminalv1.AttachResponse{Message: &terminalv1.AttachResponse_Ready{Ready: &emptypb.Empty{}}}); err != nil {
		return err
	}

	if err := stream.Send(&terminalv1.AttachResponse{Message: &terminalv1.AttachResponse_Output{Output: []byte(step.output)}}); err != nil {
		return err
	}

	if step.reset {
		panic(http.ErrAbortHandler)
	}

	if step.disconnect {
		f.closeConnections()

		return nil
	}

	if step.listen {
		for {
			request, err := stream.Receive()
			if errors.Is(err, io.EOF) {
				return nil
			}

			if err != nil {
				return err
			}

			f.heard <- request
		}
	}

	if !step.exit {
		return nil
	}

	return stream.Send(&terminalv1.AttachResponse{Message: &terminalv1.AttachResponse_Exit{Exit: &terminalv1.Exit{}}})
}

func (f *fakeAttachService) readOpened() []*terminalv1.OpenAttach {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.opened
}

func serveAttach(t *testing.T, service *fakeAttachService) terminalv1connect.TerminalServiceClient {
	t.Helper()

	mux := http.NewServeMux()
	mux.Handle(terminalv1connect.NewTerminalServiceHandler(service))
	server := httptest.NewUnstartedServer(mux)
	server.Config.Protocols = xhttp.NewServerProtocols()
	server.Start()
	service.closeConnections = server.CloseClientConnections
	t.Cleanup(server.Close)

	return terminalv1connect.NewTerminalServiceClient(xhttp.NewClientOver(xhttp.NewStreamTransport(), 0), server.URL)
}

// TestRunTerminalKeepsAQuietStreamOpen covers an attach nobody types into, behind a proxy that
// cuts a request that sends nothing.
func TestRunTerminalKeepsAQuietStreamOpen(t *testing.T) {
	service := &fakeAttachService{
		steps: []attachStep{{listen: true}},
		heard: make(chan *terminalv1.AttachRequest, 64),
	}
	client := serveAttach(t, service)

	typed, typing := io.Pipe()
	t.Cleanup(func() { _ = typing.Close() })

	released := make(chan struct{})
	asked := 0
	token := func(ctx context.Context) (string, error) {
		asked++
		if asked == 1 {
			return "fresh", nil
		}

		select {
		case <-released:
			return "fresh", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	ran := make(chan error, 1)
	go func() {
		_, err := runTerminal(ctx, runTerminalParams{
			Dial:              dialAttach(client, "agent-1", "xterm-256color"),
			Token:             token,
			KeepaliveInterval: 10 * time.Millisecond,
			In:                typed,
			Out:               io.Discard,
			Size: func() *terminalv1.Size {
				return &terminalv1.Size{Rows: 24, Cols: 80}
			},
			Resized: make(chan os.Signal),
			Ready: func() (func(), error) {
				return func() {}, nil
			},
		})
		ran <- err
	}()

	hear := func(t *testing.T) *terminalv1.AttachRequest {
		t.Helper()

		select {
		case request := <-service.heard:
			return request
		case <-time.After(5 * time.Second):
			t.Fatal("the quiet stream sent nothing")

			return nil
		}
	}

	t.Run("a quiet stream sends empty messages while the token is slow to come", func(t *testing.T) {
		for range 3 {
			assert.Nil(t, hear(t).GetMessage())
		}
	})

	t.Run("a fresh token goes up once it comes", func(t *testing.T) {
		close(released)

		for range 100 {
			if token := hear(t).GetToken(); token != "" {
				assert.Equal(t, "fresh", token)

				return
			}
		}

		t.Fatal("the fresh token never went up")
	})

	t.Run("a token already sent is not sent again", func(t *testing.T) {
		for range 10 {
			assert.Empty(t, hear(t).GetToken())
		}
	})

	t.Run("cancelling stops the keepalive and ends the terminal", func(t *testing.T) {
		cancel()

		settled := time.After(50 * time.Millisecond)
	drain:
		for {
			select {
			case <-service.heard:
			case <-settled:
				break drain
			}
		}

		select {
		case request := <-service.heard:
			t.Fatalf("a cancelled terminal still sent %v", request)
		case <-time.After(100 * time.Millisecond):
		}

		service.closeConnections()

		select {
		case <-ran:
		case <-time.After(5 * time.Second):
			t.Fatal("the terminal outlived its context")
		}
	})
}

// TestRunTerminal covers an attach from the client side, dialled again when it drops.
func TestRunTerminal(t *testing.T) {
	tests := []struct {
		name           string
		steps          []attachStep
		conns          []terminalConn
		signInsMissing int
		typed          string
		wantOut        string
		wantCode       connect.Code
		wantOpened     int
		wantHeard      string
	}{
		{
			name:      "input a lost stream took is sent on the stream dialled after it",
			conns:     []terminalConn{&lostConn{ended: make(chan struct{})}, &heardConn{sent: make(chan []byte, 1)}},
			typed:     "typed",
			wantHeard: "typed",
		},
		{
			name:       "a refused attach is not dialled again",
			steps:      []attachStep{{refuse: connect.NewError(connect.CodeNotFound, os.ErrNotExist)}},
			wantCode:   connect.CodeNotFound,
			wantOpened: 1,
		},
		{
			name:       "an attach refused after a drop stops dialling",
			steps:      []attachStep{{output: "before "}, {refuse: connect.NewError(connect.CodeFailedPrecondition, os.ErrClosed)}},
			wantOut:    "before ",
			wantCode:   connect.CodeFailedPrecondition,
			wantOpened: 2,
		},
		{
			name:       "an attach the engine fails after a drop stops dialling",
			steps:      []attachStep{{output: "before "}, {refuse: errors.New("the pty failed")}},
			wantOut:    "before ",
			wantCode:   connect.CodeUnknown,
			wantOpened: 2,
		},
		{
			name:       "an attach whose stream is reset is dialled again and resumes",
			steps:      []attachStep{{output: "before ", reset: true}, {output: "after", exit: true}},
			wantOut:    "before after",
			wantOpened: 2,
		},
		{
			name:       "an attach whose connection is closed is dialled again and resumes",
			steps:      []attachStep{{output: "before ", disconnect: true}, {output: "after", exit: true}},
			wantOut:    "before after",
			wantOpened: 2,
		},
		{
			name:       "a dropped attach is dialled again and resumes",
			steps:      []attachStep{{output: "before "}, {output: "after", exit: true}},
			wantOut:    "before after",
			wantOpened: 2,
		},
		{
			name:           "an attach opened before a sign-in waits for one",
			steps:          []attachStep{{output: "agent", exit: true}},
			signInsMissing: 2,
			wantOut:        "sign in to the engine with komrad login\nThe terminal opens once you sign in.\nagent",
			wantOpened:     1,
		},
		{
			name:       "an attach carries the agent's terminal until it exits",
			steps:      []attachStep{{output: "agent", exit: true}},
			wantOut:    "agent",
			wantOpened: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeAttachService{steps: tt.steps}
			client := serveAttach(t, service)

			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()

			typed, typing := io.Pipe()
			t.Cleanup(func() { _ = typing.Close() })
			if tt.typed != "" {
				go func() {
					_, _ = typing.Write([]byte(tt.typed))
					_ = typing.Close()
				}()
			}
			var out syncBuffer

			dial := dialAttach(client, "agent-1", "xterm-256color")
			if tt.conns != nil {
				conns := tt.conns
				dial = func(context.Context, *terminalv1.Size) (terminalConn, error) {
					conn := conns[0]
					conns = conns[1:]

					return conn, nil
				}
			}

			var token func(context.Context) (string, error)
			if tt.signInsMissing > 0 {
				missing := tt.signInsMissing
				token = func(context.Context) (string, error) {
					if missing > 0 {
						missing--

						return "", engine.ErrSignInNeeded
					}

					return "token", nil
				}
			}

			_, err := runTerminal(ctx, runTerminalParams{
				Dial:   dial,
				Token:  token,
				Redial: time.Minute,
				In:     typed,
				Out:    &out,
				Size: func() *terminalv1.Size {
					return &terminalv1.Size{Rows: 24, Cols: 80}
				},
				Resized: make(chan os.Signal),
				Ready: func() (func(), error) {
					return func() {}, nil
				},
			})

			if tt.wantCode != 0 {
				require.Error(t, err)
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantOut, out.String())

			if tt.wantHeard != "" {
				heard := tt.conns[len(tt.conns)-1].(*heardConn)
				assert.Equal(t, tt.wantHeard, string(heard.heard))
			}

			opened := service.readOpened()
			require.Len(t, opened, tt.wantOpened)
			for _, open := range opened {
				assert.Equal(t, "agent-1", open.GetAgentId())
				assert.Equal(t, "xterm-256color", open.GetTerm())
				assert.Equal(t, uint32(24), open.GetSize().GetRows())
			}
		})
	}
}

// lostConn is a stream whose connection is gone, failing the first send and then its receive.
type lostConn struct {
	ended chan struct{}
}

func (c *lostConn) sendInput([]byte) error {
	close(c.ended)

	return connect.NewError(connect.CodeUnavailable, io.ErrClosedPipe)
}

func (c *lostConn) sendResize(*terminalv1.Size) error { return nil }

func (c *lostConn) sendToken(string) error { return nil }

func (c *lostConn) sendKeepalive() error { return nil }

func (c *lostConn) closeInput() error { return nil }

func (c *lostConn) receive() (received, error) {
	<-c.ended

	return received{}, connect.NewError(connect.CodeUnavailable, io.ErrClosedPipe)
}

func (c *lostConn) close() {}

// heardConn is a stream that exits once it has been sent input or its input closes.
type heardConn struct {
	sent  chan []byte
	heard []byte
}

func (c *heardConn) sendInput(input []byte) error {
	c.sent <- input

	return nil
}

func (c *heardConn) sendResize(*terminalv1.Size) error { return nil }

func (c *heardConn) sendToken(string) error { return nil }

func (c *heardConn) sendKeepalive() error { return nil }

func (c *heardConn) closeInput() error {
	close(c.sent)

	return nil
}

func (c *heardConn) receive() (received, error) {
	c.heard = <-c.sent

	return received{exit: &terminalv1.Exit{}}, nil
}

func (c *heardConn) close() {}
