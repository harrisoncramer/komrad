package ui_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"weave/config"
	"weave/internal/client"
	"weave/internal/engine"
	"weave/internal/git"
	"weave/internal/position"
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

var reviewCursorLine = regexp.MustCompile(`^[^ ]+:\d+`)

var listBars = []string{"│ ", "┃ "}

var timerCmds = []string{
	"bubbletea/v2.Tick.",
	"bubbletea/v2.Every.",
	"cursor.(*Model).Blink.",
}

type panelHarness struct {
	t        *testing.T
	panel    *ui.Panel
	recorder *requestRecorder
	gate     *gate
	timers   []tea.Cmd
	parked   []parkedCmd
}

type gate struct {
	mu     sync.Mutex
	held   map[string]bool
	parked chan parkedCall
}

type parkedCall struct {
	name    string
	release chan struct{}
}

type parkedCmd struct {
	call parkedCall
	done chan tea.Msg
}

type heldGit struct {
	git  ui.ReviewGit
	gate *gate
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
	gate    *gate
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
	width         int
	height        int
	detailsHeight float64
	sidebarSide   string
	sidebarWidth  int
	keys          *config.Keybindings
	git           ui.ReviewGit
	settings      ui.PanelSettings
	engine        engine.Remote
	harnessFrames ui.HarnessFrameReader
	configErr     error
	startupErr    error
	refuseSplits  bool
}

func newPanelHarness(t *testing.T, params panelHarnessParams) *panelHarness {
	t.Helper()

	gate := &gate{
		held:   map[string]bool{},
		parked: make(chan parkedCall),
	}
	recorder := &requestRecorder{
		answers: map[string]answerFunc{},
		gate:    gate,
	}
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

	var reviewGit ui.ReviewGit
	if params.git != nil {
		reviewGit = heldGit{
			git:  params.git,
			gate: gate,
		}
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
		gate:     gate,
		panel: ui.NewPanel(ui.NewPanelParams{
			Agents:        agentv1connect.NewAgentServiceClient(server.Client(), server.URL),
			Workflows:     workflowv1connect.NewWorkflowServiceClient(server.Client(), server.URL),
			Schedules:     schedulev1connect.NewScheduleServiceClient(server.Client(), server.URL),
			Hooks:         hookv1connect.NewHookServiceClient(server.Client(), server.URL),
			Worktrees:     worktreev1connect.NewWorktreeServiceClient(server.Client(), server.URL),
			Git:           reviewGit,
			Keys:          keys,
			DetailsHeight: params.detailsHeight,
			SidebarSide:   params.sidebarSide,
			SidebarWidth:  params.sidebarWidth,
			Width:         width,
			Height:        height,
			Settings:      params.settings,
			Engine:        params.engine,
			HarnessFrames: params.harnessFrames,
			ConfigErr:     params.configErr,
			StartupErr:    params.startupErr,
			RefuseSplits:  params.refuseSplits,
		}),
	}
	t.Cleanup(harness.releaseParked)

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

func (h *panelHarness) assertSentTo(procedure string, want ...proto.Message) {
	h.t.Helper()

	diff := cmp.Diff(want, h.readSent(procedure), protocmp.Transform(), cmpopts.EquateEmpty())
	assert.Empty(h.t, diff, "the requests sent to %s (-want +got)", procedure)
}

func (h *panelHarness) readSent(procedure string) []proto.Message {
	h.recorder.mu.Lock()
	defer h.recorder.mu.Unlock()

	var sent []proto.Message
	for _, seen := range h.recorder.seen {
		if seen.procedure == procedure {
			sent = append(sent, seen.msg)
		}
	}

	return sent
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

		msg, isDone := h.run(next)
		if !isDone {
			continue
		}

		switch msg := msg.(type) {
		case nil, tea.QuitMsg:
		case tea.BatchMsg:
			queue = append(queue, msg...)
		default:
			queue = append(queue, h.panel.Update(msg))
		}
	}
}

func (h *panelHarness) hold(name string) {
	h.gate.mu.Lock()
	defer h.gate.mu.Unlock()

	h.gate.held[name] = true
}

func (h *panelHarness) release(name string) {
	h.t.Helper()

	for i, parked := range h.parked {
		if parked.call.name != name {
			continue
		}

		h.parked = append(h.parked[:i:i], h.parked[i+1:]...)
		close(parked.call.release)
		msg, isDone := h.await(parked.done)
		if isDone {
			h.settle(func() tea.Msg {
				return msg
			})
		}

		return
	}

	require.Failf(h.t, "nothing to release", "no call to %s is held", name)
}

func (h *panelHarness) run(cmd tea.Cmd) (tea.Msg, bool) {
	done := make(chan tea.Msg, 1)
	go func() {
		done <- cmd()
	}()

	return h.await(done)
}

func (h *panelHarness) await(done chan tea.Msg) (tea.Msg, bool) {
	select {
	case msg := <-done:
		return msg, true
	case call := <-h.gate.parked:
		h.parked = append(h.parked, parkedCmd{
			call: call,
			done: done,
		})

		return nil, false
	}
}

func (h *panelHarness) releaseParked() {
	h.gate.mu.Lock()
	clear(h.gate.held)
	h.gate.mu.Unlock()

	for _, parked := range h.parked {
		close(parked.call.release)
	}
	h.parked = nil
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

		l.gate.wait(req.Spec().Procedure)

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

func (g *gate) wait(name string) {
	g.mu.Lock()
	isHeld := g.held[name]
	g.mu.Unlock()
	if !isHeld {
		return
	}

	release := make(chan struct{})
	g.parked <- parkedCall{
		name:    name,
		release: release,
	}
	<-release
}

func (g heldGit) CountChangedFiles(ctx context.Context, params git.CountChangedFilesParams) (int, error) {
	g.gate.wait("git.CountChangedFiles")

	return g.git.CountChangedFiles(ctx, params)
}

func (g heldGit) MapRanges(ctx context.Context, params git.MapRangesParams) ([]position.Mapping, error) {
	g.gate.wait("git.MapRanges")

	return g.git.MapRanges(ctx, params)
}

func (g heldGit) Replay(ctx context.Context, params git.ReplayParams) (*git.Replayed, error) {
	g.gate.wait("git.Replay")

	return g.git.Replay(ctx, params)
}

func (g heldGit) IsAncestor(ctx context.Context, dir, maybeAncestor, of string) (bool, error) {
	g.gate.wait("git.IsAncestor")

	return g.git.IsAncestor(ctx, dir, maybeAncestor, of)
}

func (g heldGit) GetRoot(ctx context.Context, dir string) (string, error) {
	g.gate.wait("git.GetRoot")

	return g.git.GetRoot(ctx, dir)
}

func (g heldGit) Diff(ctx context.Context, params git.DiffParams) ([]byte, error) {
	g.gate.wait("git.Diff")

	return g.git.Diff(ctx, params)
}

func (g heldGit) ReadFiles(ctx context.Context, dir string, revisions []git.FileRevision) (map[git.FileRevision][]byte, error) {
	g.gate.wait("git.ReadFiles")

	return g.git.ReadFiles(ctx, dir, revisions)
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

func buildPuts(rows ...proto.Message) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Changes{Changes: &watchv1.Changes{Puts: wrapRows(rows)}}}
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
	case *agentv1.ReviewThread:
		return &watchv1.Row{Row: &watchv1.Row_ReviewThread{ReviewThread: row}}
	case *agentv1.Mail:
		return &watchv1.Row{Row: &watchv1.Row_Mail{Mail: row}}
	case *pullrequestv1.PullRequest:
		return &watchv1.Row{Row: &watchv1.Row_PullRequest{PullRequest: row}}
	case *pullrequestv1.Repository:
		return &watchv1.Row{Row: &watchv1.Row_Repository{Repository: row}}
	case *worktreev1.WatchedWorktree:
		return &watchv1.Row{Row: &watchv1.Row_Worktree{Worktree: row}}
	default:
		panic(fmt.Sprintf("no watch row holds a %T", row))
	}
}

func splitLines(frame string) []string {
	return strings.Split(frame, "\n")
}

func readListRows(frame string) []string {
	var rows []string
	for _, line := range splitLines(frame) {
		row, isRow := cutListRow(line)
		if isRow {
			rows = append(rows, row)
		}
	}

	return rows
}

func readHoveredRow(frame string) string {
	for _, line := range splitLines(frame) {
		if !strings.HasPrefix(line, "›") {
			continue
		}

		row, isRow := cutListRow(line)
		if !isRow {
			return strings.TrimSpace(strings.TrimPrefix(ansi.Truncate(line, config.DefaultSidebarWidth, ""), "›"))
		}

		return row
	}

	return ""
}

func cutListRow(line string) (string, bool) {
	sidebar := ansi.Truncate(line, config.DefaultSidebarWidth, "")
	for _, bar := range listBars {
		if _, row, isRow := strings.Cut(sidebar, bar); isRow {
			return strings.TrimRight(row, " "), true
		}
	}

	return "", false
}

func readRecord(frame string) []string {
	record, _ := cutRecord(frame)

	return record
}

func isRecordDrawn(frame string) bool {
	_, drawn := cutRecord(frame)

	return drawn
}

func cutRecord(frame string) ([]string, bool) {
	var record []string
	width, rules := 0, 0
	for _, line := range splitLines(frame) {
		if strings.HasPrefix(line, "─") {
			rules++
			if rules == 2 {
				break
			}
			width = ansi.StringWidth(line) - ansi.StringWidth(strings.TrimLeft(line, "─"))

			continue
		}
		if rules == 1 {
			record = append(record, strings.TrimRight(ansi.Truncate(line, width, ""), " "))
		}
	}
	if rules < 2 {
		return nil, false
	}
	if len(record) == 0 {
		return nil, true
	}
	if !strings.HasPrefix(record[0], "  ") && !strings.HasPrefix(record[0], "› ") {
		return nil, false
	}

	return record, true
}

func readRecordField(frame, label string) string {
	for _, line := range readRecord(frame) {
		field := strings.TrimLeft(line, "› ")
		if value, isField := strings.CutPrefix(field, label+" "); isField {
			return strings.TrimSpace(value)
		}
	}

	return ""
}

func readRecordCursor(frame string) string {
	for _, line := range readRecord(frame) {
		if field, isHovered := strings.CutPrefix(line, "› "); isHovered {
			return field
		}
	}

	return ""
}

func findRecordLine(frame, label string) int {
	for i, line := range splitLines(frame) {
		if strings.HasPrefix(strings.TrimLeft(line, "› "), label+" ") {
			return i
		}
	}

	return -1
}

func readLegend(frame string) string {
	rows := splitLines(frame)
	for i := len(rows) - 1; i >= 0; i-- {
		if strings.HasPrefix(rows[i], "─") {
			return strings.Join(rows[i+1:], "\n")
		}
	}

	return ""
}

func readSidebar(frame string) []string {
	return xslice.Map(splitLines(frame), func(line string) string {
		return strings.TrimRight(ansi.Truncate(line, config.DefaultSidebarWidth, ""), " ")
	})
}

func readSidebarProse(frame string) string {
	return strings.Join(strings.Fields(strings.Join(readSidebar(frame), " ")), " ")
}

func readStage(frame string) []string {
	return xslice.Map(splitLines(frame), func(line string) string {
		return strings.TrimRight(ansi.TruncateLeft(line, config.DefaultSidebarWidth, ""), " ")
	})
}

func readReviewCursor(frame string) string {
	for _, line := range readSidebar(frame) {
		if reviewCursorLine.MatchString(line) {
			return line
		}
	}

	return ""
}
