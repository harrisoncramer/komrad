package ui_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"komrad/config"
	"komrad/internal/browser"
	"komrad/internal/client"
	"komrad/internal/engine"
	"komrad/internal/gittest"
	"komrad/internal/loader"
	"komrad/internal/ui"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	"komrad/proto/gen/forge/v1/forgev1connect"
	"komrad/proto/gen/hook/v1/hookv1connect"
	"komrad/proto/gen/log/v1/logv1connect"
	"komrad/proto/gen/loop/v1/loopv1connect"
	paginationv1 "komrad/proto/gen/pagination/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	"komrad/proto/gen/pullrequest/v1/pullrequestv1connect"
	schedulev1 "komrad/proto/gen/schedule/v1"
	schedulev1connect "komrad/proto/gen/schedule/v1/schedulev1connect"
	taskv1 "komrad/proto/gen/task/v1"
	"komrad/proto/gen/task/v1/taskv1connect"
	watchv1 "komrad/proto/gen/watch/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"
	"komrad/proto/gen/workflow/v1/workflowv1connect"
	worktreev1 "komrad/proto/gen/worktree/v1"
	"komrad/proto/gen/worktree/v1/worktreev1connect"

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

	harness.press(slices.Concat([]string{"/"}, spellKeys("says:api"))...)
	harness.assertSent()

	harness.fireTimers()
	harness.assertSent(request{
		procedure: agentv1connect.AgentServiceSearchTranscriptsProcedure,
		msg: &agentv1.SearchTranscriptsRequest{
			Query:      `"api"`,
			Pagination: &paginationv1.Pagination{Limit: xptr.Ptr[int32](100)},
		},
	})
}

const (
	maxSettleSteps       = 1000
	defaultHarnessWidth  = 140
	defaultHarnessHeight = 40
)

var reviewCursorLine = regexp.MustCompile(`^[^ ]+:\d+`)

var taggedRule = regexp.MustCompile(`^─+ [^│┤┼]*[^─│┤┼ ] ─`)

var timerCmds = []string{
	"bubbletea/v2.Tick.",
	"bubbletea/v2.Every.",
	"cursor.(*Model).Blink.",
	"ui.fireAtOnce.",
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

type panelRemote struct {
	agents       agentv1connect.AgentServiceClient
	workflows    workflowv1connect.WorkflowServiceClient
	schedules    schedulev1connect.ScheduleServiceClient
	hooks        hookv1connect.HookServiceClient
	worktrees    worktreev1connect.WorktreeServiceClient
	logs         logv1connect.LogServiceClient
	forges       forgev1connect.ForgeServiceClient
	pullRequests pullrequestv1connect.PullRequestServiceClient
	loops        loopv1connect.LoopServiceClient
	tasks        taskv1connect.TaskServiceClient
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

type panelHarnessParams struct {
	width         int
	height        int
	detailsHeight float64
	sidebarSide   string
	sidebarWidth  int
	triageRows    int
	keys          *config.Keybindings
	git           *gittest.Repo
	settings      ui.PanelSettings
	engine        engine.Remote
	shellCommand  []string
	// terminalCommand is what the stage attaches to a remote agent with.
	terminalCommand []string
	harnessFrames   ui.HarnessFrameReader
	configErr       error
	startupErr      error
	refuseSplits    bool
	binary          ui.BinaryWatcher
	browser         browser.Opener
	tmux            ui.PanelTmux
	remote          *panelRemote
	engineStatus    ui.EngineStatus
	loader          loader.Style
}

func newPanelHarness(t *testing.T, params panelHarnessParams) *panelHarness {
	t.Helper()

	gate := &gate{
		held:   map[string]bool{},
		parked: make(chan parkedCall),
	}
	recorder := &requestRecorder{
		answers: map[string]answerFunc{taskv1connect.TaskServiceCreateTaskProcedure: echoCreatedTask},
		gate:    gate,
	}
	interceptors := connect.WithInterceptors(recorder)

	mux := http.NewServeMux()
	mux.Handle(agentv1connect.NewAgentServiceHandler(agentv1connect.UnimplementedAgentServiceHandler{}, interceptors))
	mux.Handle(workflowv1connect.NewWorkflowServiceHandler(workflowv1connect.UnimplementedWorkflowServiceHandler{}, interceptors))
	mux.Handle(schedulev1connect.NewScheduleServiceHandler(schedulev1connect.UnimplementedScheduleServiceHandler{}, interceptors))
	mux.Handle(hookv1connect.NewHookServiceHandler(hookv1connect.UnimplementedHookServiceHandler{}, interceptors))
	mux.Handle(worktreev1connect.NewWorktreeServiceHandler(worktreev1connect.UnimplementedWorktreeServiceHandler{}, interceptors))
	mux.Handle(logv1connect.NewLogServiceHandler(logv1connect.UnimplementedLogServiceHandler{}, interceptors))
	mux.Handle(forgev1connect.NewForgeServiceHandler(forgev1connect.UnimplementedForgeServiceHandler{}, interceptors))
	mux.Handle(pullrequestv1connect.NewPullRequestServiceHandler(pullrequestv1connect.UnimplementedPullRequestServiceHandler{}, interceptors))
	mux.Handle(loopv1connect.NewLoopServiceHandler(loopv1connect.UnimplementedLoopServiceHandler{}, interceptors))
	mux.Handle(taskv1connect.NewTaskServiceHandler(taskv1connect.UnimplementedTaskServiceHandler{}, interceptors))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	remote := params.remote
	if remote == nil {
		remote = &panelRemote{
			agents:       agentv1connect.NewAgentServiceClient(server.Client(), server.URL),
			workflows:    workflowv1connect.NewWorkflowServiceClient(server.Client(), server.URL),
			schedules:    schedulev1connect.NewScheduleServiceClient(server.Client(), server.URL),
			hooks:        hookv1connect.NewHookServiceClient(server.Client(), server.URL),
			worktrees:    worktreev1connect.NewWorktreeServiceClient(server.Client(), server.URL),
			logs:         logv1connect.NewLogServiceClient(server.Client(), server.URL),
			forges:       forgev1connect.NewForgeServiceClient(server.Client(), server.URL),
			pullRequests: pullrequestv1connect.NewPullRequestServiceClient(server.Client(), server.URL),
			loops:        loopv1connect.NewLoopServiceClient(server.Client(), server.URL),
			tasks:        taskv1connect.NewTaskServiceClient(server.Client(), server.URL),
		}
	}

	keys := config.DefaultKeybindings()
	if params.keys != nil {
		keys = *params.keys
	}

	var reviewGit ui.ReviewGit
	if params.git != nil {
		require.Nil(t, params.git.Before, "a repo is held by one harness")
		params.git.Before = gate.wait
		reviewGit = params.git
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
			Agents:          remote.agents,
			Workflows:       remote.workflows,
			Schedules:       remote.schedules,
			Hooks:           remote.hooks,
			Worktrees:       remote.worktrees,
			Forges:          remote.forges,
			PullRequests:    remote.pullRequests,
			Loops:           remote.loops,
			Tasks:           remote.tasks,
			Logs:            remote.logs,
			Git:             reviewGit,
			Keys:            keys,
			DetailsHeight:   params.detailsHeight,
			SidebarSide:     params.sidebarSide,
			SidebarWidth:    params.sidebarWidth,
			TriageRows:      params.triageRows,
			Width:           width,
			Height:          height,
			Settings:        draftedSettings(t, params.settings),
			Engine:          params.engine,
			ShellCommand:    params.shellCommand,
			TerminalCommand: params.terminalCommand,
			HarnessFrames:   params.harnessFrames,
			ConfigErr:       params.configErr,
			StartupErr:      params.startupErr,
			RefuseSplits:    params.refuseSplits,
			Binary:          params.binary,
			Browser:         params.browser,
			Tmux:            params.tmux,
			EngineStatus:    params.engineStatus,
			Loader:          params.loader,
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
		if key == rest {
			h.fireRest()

			continue
		}

		if to, ok := strings.CutPrefix(key, "pick:"); ok {
			h.pick(to)

			continue
		}

		h.send(ui.NewKeyPress(key))
	}
}

func (h *panelHarness) send(msg tea.Msg) {
	h.t.Helper()

	if size, ok := msg.(tea.WindowSizeMsg); ok {
		h.panel.SetSidebarSize(size.Width, size.Height)
	}
	h.settle(h.panel.Update(msg))
}

func (h *panelHarness) readFrame() string {
	return ansi.Strip(h.readRawFrame())
}

// readTypedFrame is the frame with the character a text cursor stands on written as [c].
func (h *panelHarness) readTypedFrame() string {
	return ansi.Strip(typedCursor.ReplaceAllString(h.readRawFrame(), "[$1]"))
}

func (h *panelHarness) readScreen() string {
	return h.panel.ReadScreen()
}

func (h *panelHarness) readRendered() string {
	return ansi.Strip(h.panel.View())
}

func (h *panelHarness) readRawFrame() string {
	left, width := h.panel.Inside()
	lines := strings.Split(h.panel.View(), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(emptyStyle.ReplaceAllString(ansi.Cut(line, left, left+width), ""), " ")
	}

	return strings.Join(lines, "\n")
}

func (h *panelHarness) readRequests() []request {
	h.recorder.mu.Lock()
	defer h.recorder.mu.Unlock()

	return slices.Clone(h.recorder.seen)
}

func (h *panelHarness) assertSent(want ...request) {
	h.t.Helper()
	h.assertSentWith(nil, want...)
}

func (h *panelHarness) assertSentWith(opts []cmp.Option, want ...request) {
	h.t.Helper()
	h.recorder.mu.Lock()
	defer h.recorder.mu.Unlock()

	diff := cmp.Diff(want, h.recorder.seen, append([]cmp.Option{cmp.AllowUnexported(request{}), protocmp.Transform(), cmpopts.EquateEmpty()}, opts...)...)
	assert.Empty(h.t, diff, "the requests sent (-want +got)")
}

func (h *panelHarness) assertSentTo(procedure string, want ...proto.Message) {
	h.t.Helper()
	h.assertSentToWith(procedure, nil, want...)
}

func (h *panelHarness) assertSentToWith(procedure string, opts []cmp.Option, want ...proto.Message) {
	h.t.Helper()

	diff := cmp.Diff(want, h.readSent(procedure), append([]cmp.Option{protocmp.Transform(), cmpopts.EquateEmpty()}, opts...)...)
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

// echoCreatedTask answers a create with the task it asked for, so a form can go on to start its agent.
func echoCreatedTask(req connect.AnyRequest) (connect.AnyResponse, error) {
	asked, ok := req.Any().(*taskv1.CreateTaskRequest)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, errors.New("not a task create"))
	}

	return connect.NewResponse(&taskv1.CreateTaskResponse{Task: &taskv1.Task{
		Id:         "t1",
		WorkflowId: asked.GetWorkflowId(),
		Slug:       asked.GetSlug(),
		Title:      asked.GetTitle(),
		Goal:       asked.GetGoal(),
	}}), nil
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

	for _, msg := range h.runTimers() {
		h.settle(func() tea.Msg {
			return msg
		})
	}
}

func (h *panelHarness) firePulse() {
	h.t.Helper()

	h.fireMatching(ui.IsPulse)
}

func (h *panelHarness) fireLoader() bool {
	h.t.Helper()

	return h.fireMatching(ui.IsLoaderTick)
}

// rest is a press that lets the cursor come to rest on the row it was moved to.
const rest = "<rest>"

// fireRest lets the cursor come to rest on the row it was moved to.
func (h *panelHarness) fireRest() {
	h.t.Helper()

	h.fireMatching(ui.IsStageTick)
}

func (h *panelHarness) fireMatching(is func(tea.Msg) bool) bool {
	h.t.Helper()

	pending := h.timers
	h.timers = nil
	fired := false
	for _, timer := range pending {
		msg := timer()
		if !is(msg) {
			h.timers = append(h.timers, timer)

			continue
		}

		fired = true
		h.settle(func() tea.Msg {
			return msg
		})
	}

	return fired
}

func (h *panelHarness) fireTimersUnsettled() []tea.Cmd {
	return xslice.Map(h.runTimers(), func(msg tea.Msg) tea.Cmd {
		cmd := h.panel.Update(msg)
		h.panel.View()

		return cmd
	})
}

func (h *panelHarness) runTimers() []tea.Msg {
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

	return fired
}

func (l *requestRecorder) WrapUnary(connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		l.mu.Lock()
		msg, _ := req.Any().(proto.Message)
		l.seen = append(l.seen, request{
			procedure: req.Spec().Procedure,
			msg:       msg,
		})
		answer := l.answers[req.Spec().Procedure]
		l.mu.Unlock()

		l.gate.wait(ctx, req.Spec().Procedure)

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

func (g *gate) wait(ctx context.Context, name string) {
	g.mu.Lock()
	isHeld := g.held[name]
	g.mu.Unlock()
	if !isHeld {
		return
	}

	release := make(chan struct{})
	select {
	case g.parked <- parkedCall{
		name:    name,
		release: release,
	}:
	case <-ctx.Done():
		return
	}

	select {
	case <-release:
	case <-ctx.Done():
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
	case *agentv1.DocumentType:
		return &watchv1.Row{Row: &watchv1.Row_DocumentType{DocumentType: row}}
	case *taskv1.Task:
		return &watchv1.Row{Row: &watchv1.Row_Task{Task: row}}
	default:
		panic(fmt.Sprintf("no watch row holds a %T", row))
	}
}

func splitLines(frame string) []string {
	return strings.Split(frame, "\n")
}

// readBelowViews is the frame's lines under the logo, its header and any row of views, whole
// when no logo is rendered.
func readBelowViews(frame string) []string {
	lines := splitLines(frame)
	if len(lines) <= 4 || !strings.Contains(lines[0], "\U000F050C komrad") {
		return lines
	}
	if strings.Contains(lines[3], "agents  schedules") {
		return lines[4:]
	}

	return lines[3:]
}

func readTrail(frame string) string {
	lines := splitLines(frame)
	if len(lines) == 0 {
		return ""
	}
	first, _, _ := strings.Cut(lines[0], "│")
	first = strings.TrimSpace(first)
	if trail, isLogo := strings.CutPrefix(first, "\U000F050C komrad"); isLogo {
		return strings.TrimPrefix(trail, " › ")
	}

	return first
}

func readListRows(frame string) []string {
	return xslice.Map(readListLines(frame), func(line listLine) string { return line.row })
}

func readHoveredRow(frame string) string {
	for _, line := range readListLines(frame) {
		if strings.HasPrefix(line.sidebar, "›") {
			return line.row
		}
	}

	for _, line := range splitLines(frame) {
		if strings.HasPrefix(line, "›") {
			return strings.TrimSpace(strings.TrimPrefix(ansi.Truncate(line, ui.SidebarCols, ""), "›"))
		}
	}

	return ""
}

// listLine is one row of the list a frame renders: where it stands in the frame, the sidebar's
// part of the line, and the row with the gutter every row shares cut off.
type listLine struct {
	index   int
	sidebar string
	row     string
}

// listDivider is a rule the list renders among its own rows.
var listDivider = regexp.MustCompile(`^─+ (Triage|All Agents) ─`)

// gutterToken is what a list's gutter holds: the focus, a glyph, a loader frame or a figure.
var gutterToken = regexp.MustCompile(`^(›|[\x{E000}-\x{F8FF}\x{F0000}-\x{FFFFD}\x{2800}-\x{28FF}]|[0-9][0-9.]*[KMGTkhdwmsy]?|—)$`)

// readListLines reads the rows between the search prompt and the first rule the list does not
// render itself, cutting them all at the leftmost column a name opens at.
func readListLines(frame string) []listLine {
	lines := splitLines(frame)
	start := slices.IndexFunc(lines, func(line string) bool {
		prompt := strings.TrimSpace(ansi.Truncate(line, ui.SidebarCols, ""))

		return strings.HasPrefix(prompt, "/") || strings.HasPrefix(prompt, "▽")
	})
	if start < 0 {
		return nil
	}

	var found []listLine
	for i := start + 1; i < len(lines); i++ {
		sidebar := strings.TrimRight(ansi.Truncate(lines[i], ui.SidebarCols, ""), " ")
		if strings.HasPrefix(sidebar, "─") {
			if listDivider.MatchString(sidebar) {
				continue
			}

			break
		}
		if !isListRow(sidebar) {
			continue
		}

		found = append(found, listLine{index: i, sidebar: sidebar})
	}

	if len(found) == 0 {
		return nil
	}

	column := ui.SidebarCols
	for _, line := range found {
		column = min(column, findNameColumn(line.sidebar))
	}

	for i := range found {
		found[i].row = ansi.Cut(found[i].sidebar, column, ui.SidebarCols)
	}

	return found
}

// isListRow reports whether a line opens on the focus column with a cell inside the gutter, which
// the line a list says over no rows and text set across the pane do not.
func isListRow(sidebar string) bool {
	indent := len(sidebar) - len(strings.TrimLeft(sidebar, " "))

	return sidebar != "" && !scrollCount.MatchString(sidebar) && indent <= listRowIndent && (indent > 0 || strings.HasPrefix(sidebar, "›"))
}

// scrollCount is the line a scrolled list says how many rows lie past its edge on.
var scrollCount = regexp.MustCompile(`^\s*[↑↓] [0-9]+ more`)

// listRowIndent is the most a row leaves blank before its first cell.
const listRowIndent = 16

// findNameColumn is the column a row's name opens at, past every token of the gutter.
func findNameColumn(sidebar string) int {
	at := 0
	for _, field := range strings.SplitAfter(sidebar, " ") {
		token := strings.TrimSpace(field)
		if token != "" && !gutterToken.MatchString(token) {
			return at
		}

		at += ansi.StringWidth(field)
	}

	return at
}

func readRecord(frame string) []string {
	record, _ := cutRecord(frame)

	return record
}

func isRecordRendered(frame string) bool {
	_, rendered := cutRecord(frame)

	return rendered
}

var emptyStyle = regexp.MustCompile(`(\x1b\[[0-9;]+m\x1b\[m)+$`)

// typedCursor is a character rendered in reverse, which is how a text cursor stands on one.
var typedCursor = regexp.MustCompile(`\x1b\[(?:[0-9]+;)*7(?:;[0-9]+)*m(.)\x1b\[m`)

func cutStyled(line string, width int) string {
	return emptyStyle.ReplaceAllString(ansi.Truncate(line, width, ""), "")
}

func ruleWidth(line string) int {
	if tagged := taggedRule.FindString(line); tagged != "" {
		line = strings.Repeat("─", ansi.StringWidth(tagged)) + line[len(tagged):]
	}
	rest := strings.TrimLeft(line, "─")
	width := ansi.StringWidth(line) - ansi.StringWidth(rest)
	if strings.HasPrefix(rest, "┤") || strings.HasPrefix(rest, "┼") {
		width--
	}

	return width
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
			width = ruleWidth(line)

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
	return xslice.Map(readBelowViews(frame), func(line string) string {
		return strings.TrimRight(ansi.Truncate(line, ui.SidebarCols, ""), " ")
	})
}

func readSidebarProse(frame string) string {
	return joinProse(readSidebar(frame))
}

// readPopup is the rows inside the review's popup, empty while none is rendered.
func readPopup(frame string) []string {
	lines := splitLines(frame)
	for top, line := range lines {
		start, end := strings.Index(line, "╭"), strings.Index(line, "╮")
		if start < 0 || end < 0 {
			continue
		}

		left, right := ansi.StringWidth(line[:start]), ansi.StringWidth(line[:end])+1
		var rows []string
		for _, row := range lines[top+1:] {
			cell := ansi.Cut(row, left, right)
			if strings.HasPrefix(cell, "╰") {
				return rows
			}

			rows = append(rows, strings.TrimSpace(strings.Trim(cell, "│")))
		}

		return rows
	}

	return nil
}

// readPopupMiddle is the cell at the middle of the review's popup in a frame rendered whole.
func readPopupMiddle(frame string) (int, int) {
	lines := splitLines(frame)
	for top, line := range lines {
		start := strings.Index(line, "╭")
		if start < 0 {
			continue
		}

		left, width := ansi.StringWidth(line[:start]), ansi.StringWidth(line[start:strings.Index(line, "╮")])
		height := slices.IndexFunc(lines[top:], func(row string) bool { return strings.Contains(row, "╰") })

		return left + width/2, top + height/2
	}

	return -1, -1
}

func readPopupProse(frame string) string {
	return joinProse(readPopup(frame))
}

func joinProse(lines []string) string {
	return strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
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

// draftedSettings keeps the copies a panel writes for an editor inside the test's own directory.
func draftedSettings(t *testing.T, settings ui.PanelSettings) ui.PanelSettings {
	t.Helper()

	if settings.DraftDir == "" {
		settings.DraftDir = t.TempDir()
	}

	return settings
}

// pick lands the keys on a sidebar block, list, record or block, or on a pane, through the pane
// picker, and is what press does with a key spelled pick:<to>.
func (h *panelHarness) pick(to string) {
	h.t.Helper()

	h.send(ui.NewKeyPress("ctrl+space"))
	h.send(ui.NewKeyPress("j"))

	letter := h.panel.ReadPickLetter(to)
	if letter == "" {
		h.t.Fatalf("the picker rendered no letter over %s", to)
	}

	h.send(ui.NewKeyPress(letter))
}

func readRawListRow(frame, name string) string {
	lines := splitLines(frame)
	for _, line := range readListLines(ansi.Strip(frame)) {
		if strings.Contains(line.row, name) {
			return ansi.Truncate(lines[line.index], ui.SidebarCols, "")
		}
	}

	return ""
}
