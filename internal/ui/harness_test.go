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

	"weave/config"
	"weave/internal/client"
	"weave/internal/ui"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	"weave/proto/gen/hook/v1/hookv1connect"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
	schedulev1 "weave/proto/gen/schedule/v1"
	schedulev1connect "weave/proto/gen/schedule/v1/schedulev1connect"
	watchv1 "weave/proto/gen/watch/v1"
	workflowv1 "weave/proto/gen/workflow/v1"
	"weave/proto/gen/workflow/v1/workflowv1connect"
	worktreev1 "weave/proto/gen/worktree/v1"
	"weave/proto/gen/worktree/v1/worktreev1connect"

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

const maxSettleSteps = 1000

var timerCmds = []string{
	"bubbletea/v2.Tick.",
	"bubbletea/v2.Every.",
	"cursor.(*Model).Blink.",
}

// panelHarness drives the panel from the outside: watch batches and keys in, frames and the
// requests it sent out.
type panelHarness struct {
	t     *testing.T
	panel *ui.Panel
	log   *requestLog
}

type panelHarnessParams struct {
	width  int
	height int
	keys   *config.Keybindings
}

type request struct {
	procedure string
	msg       proto.Message
}

type answerFunc func(req connect.AnyRequest) (connect.AnyResponse, error)

// requestLog records every request the panel sends and answers it, with an empty response unless
// a test said otherwise.
type requestLog struct {
	mu      sync.Mutex
	seen    []request
	answers map[string]answerFunc
}

func newPanelHarness(t *testing.T, params panelHarnessParams) *panelHarness {
	t.Helper()

	log := &requestLog{answers: map[string]answerFunc{}}
	interceptors := connect.WithInterceptors(log)

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
		width, height = 140, 40
	}

	h := &panelHarness{
		t:   t,
		log: log,
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

	h.settle(h.panel.Init())
	h.send(tea.WindowSizeMsg{
		Width:  width,
		Height: height,
	})

	return h
}

// watch delivers one batch off the watch stream.
func (h *panelHarness) watch(events ...*watchv1.WatchEvent) {
	h.t.Helper()

	h.send(client.WatchBatch(events))
}

// press types each key in turn, letting what each one started land before the next.
func (h *panelHarness) press(keys ...string) {
	h.t.Helper()

	for _, key := range keys {
		h.send(ui.KeyPress(key))
	}
}

func (h *panelHarness) send(msg tea.Msg) {
	h.t.Helper()

	h.settle(h.panel.Update(msg))
}

// frame is the panel as drawn, without its colours.
func (h *panelHarness) frame() string {
	return ansi.Strip(h.panel.View())
}

// assertSent checks the panel sent exactly these requests, in this order.
func (h *panelHarness) assertSent(want ...request) {
	h.t.Helper()
	h.log.mu.Lock()
	defer h.log.mu.Unlock()

	diff := cmp.Diff(want, h.log.seen, cmp.AllowUnexported(request{}), protocmp.Transform(), cmpopts.EquateEmpty())
	assert.Empty(h.t, diff, "the requests sent (-want +got)")
}

// answer replaces the empty response to a procedure.
func (h *panelHarness) answer(procedure string, answer answerFunc) {
	h.log.mu.Lock()
	defer h.log.mu.Unlock()

	h.log.answers[procedure] = answer
}

// refuse answers a procedure with an error.
func (h *panelHarness) refuse(procedure string, err error) {
	h.answer(procedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return nil, err
	})
}

// settle runs what the panel asked for and hands it each message that comes back, the way the
// program does, holding back the timers so nothing waits on a clock.
func (h *panelHarness) settle(cmd tea.Cmd) {
	h.t.Helper()

	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0; steps++ {
		require.Less(h.t, steps, maxSettleSteps, "the panel never settled")

		next := queue[0]
		queue = queue[1:]
		if next == nil || isTimer(next) {
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

func (l *requestLog) WrapUnary(connect.UnaryFunc) connect.UnaryFunc {
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

func (l *requestLog) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (l *requestLog) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
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

// snapshot is the batch a stream opens with, holding every row the panel is to know about.
func snapshot(rows ...proto.Message) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Snapshot{Snapshot: &watchv1.Snapshot{Rows: watchRows(rows)}}}
}

// put is a change adding or replacing rows.
func put(rows ...proto.Message) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Changes{Changes: &watchv1.Changes{Puts: watchRows(rows)}}}
}

// deleted is a change taking a row away.
func deleted(resource watchv1.Resource, key string) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Changes{Changes: &watchv1.Changes{Deleted: []*watchv1.Deleted{{
		Resource: resource,
		Key:      key,
	}}}}}
}

func watchRows(rows []proto.Message) []*watchv1.Row {
	wrapped := make([]*watchv1.Row, 0, len(rows))
	for _, row := range rows {
		wrapped = append(wrapped, watchRow(row))
	}

	return wrapped
}

func watchRow(row proto.Message) *watchv1.Row {
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
