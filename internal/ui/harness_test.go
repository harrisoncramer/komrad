package ui_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"weave/config"
	"weave/internal/client"
	"weave/internal/ui"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	"weave/proto/gen/hook/v1/hookv1connect"
	paginationv1 "weave/proto/gen/pagination/v1"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
	schedulev1 "weave/proto/gen/schedule/v1"
	schedulev1connect "weave/proto/gen/schedule/v1/schedulev1connect"
	watchv1 "weave/proto/gen/watch/v1"
	workflowv1 "weave/proto/gen/workflow/v1"
	"weave/proto/gen/workflow/v1/workflowv1connect"
	worktreev1 "weave/proto/gen/worktree/v1"
	"weave/proto/gen/worktree/v1/worktreev1connect"

	"charm.land/bubbles/v2/cursor"
	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	maxSettleSteps       = 1000
	defaultHarnessWidth  = 140
	defaultHarnessHeight = 40
)

var timerCmds = []string{
	"bubbletea/v2.Tick.",
	"bubbletea/v2.Every.",
	"cursor.(*Model).Blink.",
}

type panelHarness struct {
	t        *testing.T
	panel    *ui.Panel
	recorder *requestRecorder
	timers   []tea.Cmd
}

type request struct {
	procedure string
	msg       proto.Message
}

type answerFunc func(req connect.AnyRequest) (connect.AnyResponse, error)

type requestRecorder struct {
	mu      sync.Mutex
	seen    []request
	answers map[string]answerFunc
}

// TestHarnessHoldsTimers covers the harness telling a timer from a command it can run at once.
func TestHarnessHoldsTimers(t *testing.T) {
	blinker := cursor.New()

	tests := []struct {
		name string
		cmd  tea.Cmd
		want bool
	}{
		{
			name: "a command that answers at once is run",
			cmd: func() tea.Msg {
				return nil
			},
			want: false,
		},
		{
			name: "a tick is held",
			cmd:  tea.Tick(time.Hour, nil),
			want: true,
		},
		{
			name: "an every is held",
			cmd:  tea.Every(time.Hour, nil),
			want: true,
		},
		{
			name: "a cursor blink is held",
			cmd:  blinker.Blink(),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NotNil(t, tt.cmd)

			assert.Equal(t, tt.want, isTimer(tt.cmd))
		})
	}
}

// TestHarnessFiresHeldTimers covers a debounced search waiting for its timer to be fired.
func TestHarnessFiresHeldTimers(t *testing.T) {
	harness := newPanelHarness(t, panelHarnessParams{})
	harness.watch(buildSnapshot())

	harness.press("s", "a", "p", "i")
	harness.assertSent()

	harness.fireTimers()
	harness.assertSent(request{
		procedure: agentv1connect.AgentServiceSearchTranscriptsProcedure,
		msg: &agentv1.SearchTranscriptsRequest{
			Query:      "api",
			Pagination: &paginationv1.Pagination{Limit: proto.Int32(10)},
		},
	})
}

type panelHarnessParams struct {
	width  int
	height int
	keys   *config.Keybindings
}

func newPanelHarness(t *testing.T, params panelHarnessParams) *panelHarness {
	t.Helper()

	recorder := &requestRecorder{answers: map[string]answerFunc{}}
	interceptors := connect.WithInterceptors(recorder)

	mux := http.NewServeMux()
	mux.Handle(agentv1connect.NewAgentServiceHandler(agentv1connect.UnimplementedAgentServiceHandler{}, interceptors))
	mux.Handle(workflowv1connect.NewWorkflowServiceHandler(workflowv1connect.UnimplementedWorkflowServiceHandler{}, interceptors))
	mux.Handle(schedulev1connect.NewScheduleServiceHandler(schedulev1connect.UnimplementedScheduleServiceHandler{}, interceptors))
	mux.Handle(hookv1connect.NewHookServiceHandler(hookv1connect.UnimplementedHookServiceHandler{}, interceptors))
	mux.Handle(worktreev1connect.NewWorktreeServiceHandler(worktreev1connect.UnimplementedWorktreeServiceHandler{}, interceptors))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	keys := config.DefaultKeybindings()
	if params.keys != nil {
		keys = *params.keys
	}

	width, height := params.width, params.height
	if width == 0 {
		width = defaultHarnessWidth
	}
	if height == 0 {
		height = defaultHarnessHeight
	}

	harness := &panelHarness{
		t:        t,
		recorder: recorder,
		panel: ui.NewPanel(ui.NewPanelParams{
			Agents:    agentv1connect.NewAgentServiceClient(server.Client(), server.URL),
			Workflows: workflowv1connect.NewWorkflowServiceClient(server.Client(), server.URL),
			Schedules: schedulev1connect.NewScheduleServiceClient(server.Client(), server.URL),
			Hooks:     hookv1connect.NewHookServiceClient(server.Client(), server.URL),
			Worktrees: worktreev1connect.NewWorktreeServiceClient(server.Client(), server.URL),
			Keys:      keys,
			Width:     width,
			Height:    height,
		}),
	}

	harness.settle(harness.panel.Init())
	harness.send(tea.WindowSizeMsg{
		Width:  width,
		Height: height,
	})

	return harness
}

func (h *panelHarness) watch(events ...*watchv1.WatchEvent) {
	h.t.Helper()

	h.send(client.WatchBatch(events))
}

func (h *panelHarness) press(keys ...string) {
	h.t.Helper()

	for _, key := range keys {
		h.send(ui.NewKeyPress(key))
	}
}

func (h *panelHarness) send(msg tea.Msg) {
	h.t.Helper()

	h.settle(h.panel.Update(msg))
}

func (h *panelHarness) readFrame() string {
	return ansi.Strip(h.panel.View())
}

func (h *panelHarness) assertSent(want ...request) {
	h.t.Helper()
	h.recorder.mu.Lock()
	defer h.recorder.mu.Unlock()

	diff := cmp.Diff(want, h.recorder.seen, cmp.AllowUnexported(request{}), protocmp.Transform(), cmpopts.EquateEmpty())
	assert.Empty(h.t, diff, "the requests sent (-want +got)")
}

func (h *panelHarness) answer(procedure string, answer answerFunc) {
	h.recorder.mu.Lock()
	defer h.recorder.mu.Unlock()

	h.recorder.answers[procedure] = answer
}

func (h *panelHarness) refuse(procedure string, err error) {
	h.answer(procedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return nil, err
	})
}

func (h *panelHarness) settle(cmd tea.Cmd) {
	h.t.Helper()

	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0; steps++ {
		require.Less(h.t, steps, maxSettleSteps, "the panel never settled")

		next := queue[0]
		queue = queue[1:]
		if next == nil {
			continue
		}
		if isTimer(next) {
			h.timers = append(h.timers, next)

			continue
		}

		switch msg := next().(type) {
		case nil, tea.QuitMsg:
		case tea.BatchMsg:
			queue = append(queue, msg...)
		default:
			queue = append(queue, h.panel.Update(msg))
		}
	}
}

func (h *panelHarness) fireTimers() {
	h.t.Helper()

	pending := h.timers
	h.timers = nil

	fired := make([]tea.Msg, len(pending))
	var group sync.WaitGroup
	for i, timer := range pending {
		group.Go(func() {
			fired[i] = timer()
		})
	}
	group.Wait()

	for _, msg := range fired {
		h.settle(func() tea.Msg {
			return msg
		})
	}
}

func (l *requestRecorder) WrapUnary(connect.UnaryFunc) connect.UnaryFunc {
	return func(_ context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		l.mu.Lock()
		msg, _ := req.Any().(proto.Message)
		l.seen = append(l.seen, request{
			procedure: req.Spec().Procedure,
			msg:       msg,
		})
		answer := l.answers[req.Spec().Procedure]
		l.mu.Unlock()

		if answer != nil {
			return answer(req)
		}

		return connect.NewResponse(&emptypb.Empty{}), nil
	}
}

func (l *requestRecorder) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (l *requestRecorder) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		l.mu.Lock()
		l.seen = append(l.seen, request{procedure: conn.Spec().Procedure})
		l.mu.Unlock()

		return next(ctx, conn)
	}
}

func isTimer(cmd tea.Cmd) bool {
	name := runtime.FuncForPC(reflect.ValueOf(cmd).Pointer()).Name()
	for _, timer := range timerCmds {
		if strings.Contains(name, timer) {
			return true
		}
	}

	return false
}

func buildSnapshot(rows ...proto.Message) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Snapshot{Snapshot: &watchv1.Snapshot{Rows: wrapRows(rows)}}}
}

func wrapRows(rows []proto.Message) []*watchv1.Row {
	return xslice.Map(rows, wrapRow)
}

func wrapRow(row proto.Message) *watchv1.Row {
	switch row := row.(type) {
	case *agentv1.Agent:
		return &watchv1.Row{Row: &watchv1.Row_Agent{Agent: row}}
	case *workflowv1.Workflow:
		return &watchv1.Row{Row: &watchv1.Row_Workflow{Workflow: row}}
	case *schedulev1.Schedule:
		return &watchv1.Row{Row: &watchv1.Row_Schedule{Schedule: row}}
	case *agentv1.Mark:
		return &watchv1.Row{Row: &watchv1.Row_Mark{Mark: row}}
	case *agentv1.WatchedReview:
		return &watchv1.Row{Row: &watchv1.Row_Review{Review: row}}
	case *agentv1.Mail:
		return &watchv1.Row{Row: &watchv1.Row_Mail{Mail: row}}
	case *pullrequestv1.PullRequest:
		return &watchv1.Row{Row: &watchv1.Row_PullRequest{PullRequest: row}}
	case *worktreev1.WatchedWorktree:
		return &watchv1.Row{Row: &watchv1.Row_Worktree{Worktree: row}}
	default:
		panic(fmt.Sprintf("no watch row holds a %T", row))
	}
}
