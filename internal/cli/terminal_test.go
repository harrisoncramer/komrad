package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
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

type attachStep struct {
	refuse error
	output string
	exit   bool
}

type fakeAttachService struct {
	terminalv1connect.UnimplementedTerminalServiceHandler

	mu     sync.Mutex
	steps  []attachStep
	opened []*terminalv1.OpenAttach
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

// TestRunTerminal covers an attach from the client side, dialled again when it drops.
func TestRunTerminal(t *testing.T) {
	tests := []struct {
		name       string
		steps      []attachStep
		wantOut    string
		wantCode   connect.Code
		wantOpened int
	}{
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
			name:       "a dropped attach is dialled again and resumes",
			steps:      []attachStep{{output: "before "}, {output: "after", exit: true}},
			wantOut:    "before after",
			wantOpened: 2,
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
			mux := http.NewServeMux()
			mux.Handle(terminalv1connect.NewTerminalServiceHandler(service))
			server := httptest.NewUnstartedServer(mux)
			server.Config.Protocols = xhttp.NewServerProtocols()
			server.Start()
			t.Cleanup(server.Close)
			client := terminalv1connect.NewTerminalServiceClient(xhttp.NewClientOver(xhttp.NewStreamTransport(), 0), server.URL)

			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()

			typed, typing := io.Pipe()
			t.Cleanup(func() { _ = typing.Close() })
			var out syncBuffer

			_, err := runTerminal(ctx, runTerminalParams{
				Dial:   dialAttach(client, "agent-1", "xterm-256color"),
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
