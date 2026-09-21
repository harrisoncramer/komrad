package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"weave/internal/frames"
	"weave/internal/git"
	"weave/internal/harness"
	"weave/internal/logging"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/storetest"
	"weave/internal/tmux"
	"weave/lib/xptr"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type fakeTmux struct {
	mu             sync.Mutex
	owners         map[string]string
	captures       map[string]string
	commands       map[string]string
	clientDir      string
	sent           map[string][][]string
	globals        map[string]string
	newWindows     map[string]string
	killedPanes    []string
	detached       []string
	sessionOptions map[string]string
	newWindowErr   error
	// beforeNewWindow runs before a window is opened.
	beforeNewWindow func()
	newWindowCalls  int
	setOwnerErr     error
	sendKeysErr     error
	paneOwnersErr   error
	respawnErr      error
	shell           string
	respawned       map[string][]respawn
}

func newFakeTmux() *fakeTmux {
	return &fakeTmux{
		owners:         map[string]string{},
		captures:       map[string]string{},
		commands:       map[string]string{},
		sent:           map[string][][]string{},
		globals:        map[string]string{},
		newWindows:     map[string]string{},
		sessionOptions: map[string]string{},
		respawned:      map[string][]respawn{},
		shell:          "/bin/zsh",
	}
}

type respawn struct {
	dir  string
	argv []string
}

func (f *fakeTmux) SetGlobal(name, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.globals[name] = value
}

func (f *fakeTmux) ShowGlobal(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.globals[name]
}

func (f *fakeTmux) GetPaneOwner(pane string) string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.owners[pane]
}

func (f *fakeTmux) CapturePane(pane string) string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.captures[pane]
}

func (f *fakeTmux) GetHostPath() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.clientDir
}

func (f *fakeTmux) DetachSession(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.detached = append(f.detached, name)
}

func (f *fakeTmux) GetPanes() (map[string]tmux.PaneStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.paneOwnersErr != nil {
		return nil, f.paneOwnersErr
	}

	panes := map[string]tmux.PaneStatus{}
	for pane, owner := range f.owners {
		panes[pane] = tmux.PaneStatus{Owner: owner, Command: f.commands[pane]}
	}

	return panes, nil
}

func (f *fakeTmux) SetSessionOption(target, name, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessionOptions[target+"/"+name] = value
}

func (f *fakeTmux) SendKeys(target string, keys ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.sendKeysErr != nil {
		return f.sendKeysErr
	}
	if target == "" {
		return errors.New("refusing to send keys to an empty target")
	}
	f.sent[target] = append(f.sent[target], keys)

	return nil
}

func (f *fakeTmux) GetDefaultShell() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.shell
}

func (f *fakeTmux) RespawnPane(pane, dir string, argv ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.respawnErr != nil {
		return f.respawnErr
	}
	if pane == "" {
		return errors.New("refusing to respawn an empty pane")
	}
	f.respawned[pane] = append(f.respawned[pane], respawn{dir: dir, argv: argv})

	return nil
}

func (f *fakeTmux) lastRespawn(pane string) respawn {
	f.mu.Lock()
	defer f.mu.Unlock()

	runs := f.respawned[pane]
	if len(runs) == 0 {
		return respawn{}
	}

	return runs[len(runs)-1]
}

func (f *fakeTmux) SplitRight(anchorPane, dir string) string { return "%stage" }

func (f *fakeTmux) NewWindow(session, name, dir string) (string, error) {
	if f.beforeNewWindow != nil {
		f.beforeNewWindow()
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.newWindowErr != nil {
		return "", f.newWindowErr
	}

	f.newWindowCalls++

	f.newWindows[name] = dir
	opened := "%" + name
	f.owners[opened] = ""

	return opened, nil
}

func (f *fakeTmux) windowsOpened() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.newWindowCalls
}

func (f *fakeTmux) SetPaneOwner(pane, agentID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.setOwnerErr != nil {
		return f.setOwnerErr
	}

	f.owners[pane] = agentID

	return nil
}

func (f *fakeTmux) KillPane(pane string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.killedPanes = append(f.killedPanes, pane)
	delete(f.owners, pane)
}

func (f *fakeTmux) IsPaneInMode(string) bool { return false }

func (f *fakeTmux) IsPaneRunningCLI(pane string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	command := f.commands[pane]

	return command != "" && !tmux.PaneStatus{Command: command}.IsRunningShell()
}

func (f *fakeTmux) IsClientAtPane(string, time.Duration) bool { return false }

func (f *fakeTmux) PasteAndSubmit(target, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent[target] = append(f.sent[target], []string{text})

	return nil
}

func (f *fakeTmux) CutLine(string) error { return nil }

func (f *fakeTmux) YankLine(string) error { return nil }

const (
	busyFrame         = "frame: working"
	idleFrame         = "frame: idle"
	composingFrame    = "frame: composing"
	waitFrame         = "frame: waiting"
	unrecognizedFrame = "frame: unrecognized"
)

const testHarnessID = "claude"

// fakeFrameRecorder stands in for the recording, holding what a pass handed it rather than
// writing anything.
type fakeFrameRecorder struct {
	mu        sync.Mutex
	recorded  []frames.RecordParams
	forgotten int
}

func (f *fakeFrameRecorder) Record(params frames.RecordParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.recorded = append(f.recorded, params)

	return nil
}

func (f *fakeFrameRecorder) Forget(map[string]struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.forgotten++
}

type fakeHarness struct {
	mu         sync.Mutex
	launches   []harness.Launch
	commandErr error
	id         string

	// noSession stands in for Codex, which takes no session id on the command line.
	noSession bool
}

func (f *fakeHarness) ID() string {
	if f.id != "" {
		return f.id
	}

	return testHarnessID
}

func (f *fakeHarness) Binary() string { return f.ID() }

func (f *fakeHarness) BuildCommand(launch harness.Launch) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.launches = append(f.launches, launch)
	if f.commandErr != nil {
		return "", f.commandErr
	}

	return "agent-cli --session " + launch.SessionID, nil
}

func (f *fakeHarness) MintSessionID() string {
	if f.noSession {
		return ""
	}

	return uuid.NewString()
}

func (f *fakeHarness) InterruptKeys() []string { return []string{"Escape"} }

func (f *fakeHarness) ReadReport(harness.Event, []byte) (harness.Report, error) {
	return harness.Report{}, nil
}

func (f *fakeHarness) ReadInput(frame string) harness.Input {
	switch {
	case frame == busyFrame:
		return harness.Input{Activity: harness.ActivityWorking}
	case frame == waitFrame:
		return harness.Input{Activity: harness.ActivityWaiting}
	case strings.HasPrefix(frame, composingFrame):
		return harness.Input{Activity: harness.ActivityComposing, Held: composingLines(strings.TrimPrefix(frame, composingFrame))}
	case frame == "":
		return harness.Input{Activity: harness.ActivityUnknown}
	case strings.HasPrefix(frame, unrecognizedFrame):
		return harness.Input{Activity: harness.ActivityUnrecognized}
	default:
		return harness.Input{Activity: harness.ActivityIdle, Held: []string{""}}
	}
}

// composingLines is the box's lines out of the frame a test wrote.
func composingLines(held string) []string {
	if held == "" {
		return []string{"half a question about"}
	}

	return strings.Split(strings.TrimPrefix(held, ": "), "|")
}

func (f *fakeHarness) LogPath(string) string { return "" }

func (f *fakeHarness) FindSession(string) (string, error) { return "", nil }

func (f *fakeHarness) ReadLog(string, int64) (harness.LogRead, error) {
	return harness.LogRead{}, nil
}

type fakeHarnesses struct {
	harness *fakeHarness
	byID    map[string]*fakeHarness
	only    []string
}

func (f fakeHarnesses) GetByID(id string) (harness.Harness, error) {
	if len(f.only) > 0 && !slices.Contains(f.only, id) {
		return nil, fmt.Errorf("%w: %q", harness.ErrNotRegistered, id)
	}
	if found, ok := f.byID[id]; ok {
		return found, nil
	}

	return f.harness, nil
}

type testStore struct {
	*storetest.Stores
}

func newStore(t *testing.T) *testStore {
	t.Helper()

	return &testStore{Stores: storetest.Open(t)}
}

func (s *testStore) Get(ctx context.Context, name string) (*agentstore.Agent, error) {
	return s.LiveAgentByName(ctx, name)
}

// Create records an agent, filing it under a workflow of its own when the test
// did not name one.
func (s *testStore) Create(ctx context.Context, agent agentstore.Agent) error {
	if agent.Harness == "" {
		agent.Harness = testHarnessID
	}
	if agent.WorkflowID == "" {
		created, err := s.Workflows.Create(ctx, workflowstore.Config{Name: "workflow-" + uuid.NewString()})
		if err != nil {
			return err
		}

		agent.WorkflowID = created.ID
	}

	return s.Agents.Create(ctx, agent)
}

// Update rewrites an agent the test already recorded.
func (s *testStore) Update(ctx context.Context, agent agentstore.Agent) error {
	return s.Agents.Update(ctx, agent)
}

func (s *testStore) List(ctx context.Context) ([]agentstore.Agent, error) {
	return s.Agents.List(ctx)
}

func (s *testStore) ListAll(ctx context.Context) ([]agentstore.Agent, error) {
	return s.Agents.ListAll(ctx)
}

// SoftDelete archives the live agent with this name.
func (s *testStore) SoftDelete(ctx context.Context, name string) error {
	agent, err := s.LiveAgentByName(ctx, name)
	if err != nil {
		return err
	}
	if agent == nil {
		return fmt.Errorf("no live agent called %q", name)
	}

	_, err = s.Agents.Archive(ctx, []string{agent.ID})

	return err
}

func (s *testStore) GetWorkflow(ctx context.Context, id string) (*workflowstore.Workflow, error) {
	return s.Workflows.Get(ctx, id)
}

func (s *testStore) GetTranscript(ctx context.Context, agentID string) (string, error) {
	return s.Transcripts.Get(ctx, agentID)
}

// testAgentStore is the one store these tests hand to all four services at once, and no
// production type is expected to satisfy the whole of it.
type testAgentStore interface {
	launchAgentReadWriter
	resumeAgentReadWriter
	adoptAgentReadWriter
	lifecycleAgentReadWriter
}

// testAgents is what a test drives instead of one service, holding the pieces an AgentService
// used to be.
type testAgents struct {
	launch       *LaunchService
	resume       *ResumeService
	adopt        *AdoptService
	lifecycle    *LifecycleService
	panes        *PaneLiveness
	emptied      *EmptyWorkflowDeleter
	orchestrator *OrchestratorService
	snapshots    *recordingSnapshots
}

type agentServiceOptions struct {
	tmux        fakeTmuxController
	harnesses   fakeHarnesses
	addr        string
	flags       string
	lifecycle   agentScriptRunner
	maxAgents   int
	maxChildren int
	logsDir     string
	framesDir   string
	agents      testAgentStore
	tx          txRunner
	stateDir    string
}

func (s *testStore) agentService(options agentServiceOptions) *testAgents {
	agents := options.agents
	if agents == nil {
		agents = s.Agents
	}

	tx := options.tx
	if tx == nil {
		tx = s.Pool
	}

	launcher := NewLauncher(NewLauncherParams{Tmux: options.tmux, Addr: options.addr})
	emptied := NewEmptyWorkflowDeleter(NewEmptyWorkflowDeleterParams{
		Workflows: s.Workflows,
		Agents:    s.Agents,
		Schedules: s.Schedules,
	})

	launch := NewLaunchService(NewLaunchServiceParams{
		Tmux:               options.tmux,
		Agents:             agents,
		Reviews:            reviewstore.New(s.Pool.GetDB()),
		Workflows:          s.Workflows,
		Tx:                 tx,
		Launcher:           launcher,
		Harnesses:          options.harnesses,
		Scripts:            options.lifecycle,
		UserDefaultHarness: testHarnessID,
		UserDefaultFlags:   map[string]string{testHarnessID: options.flags},
		MaxAgents:          options.maxAgents,
		MaxChildren:        options.maxChildren,
	})

	snapshots := &recordingSnapshots{}

	built := &testAgents{
		launch:    launch,
		snapshots: snapshots,
		resume: NewResumeService(NewResumeServiceParams{
			Tmux:      options.tmux,
			Agents:    agents,
			Workflows: s.Workflows,
			Launcher:  launcher,
			Harnesses: options.harnesses,
			Scripts:   options.lifecycle,
		}),
		adopt: NewAdoptService(NewAdoptServiceParams{
			Agents:    agents,
			Workflows: s.Workflows,
			Launch:    launch,
			Worktrees: onlyTheMainCheckout{},
		}),
		lifecycle: NewLifecycleService(NewLifecycleServiceParams{
			Tmux:           options.tmux,
			Agents:         agents,
			Workflows:      s.Workflows,
			EmptyWorkflows: emptied,
			Tx:             tx,
			Scripts:        options.lifecycle,
			Reviews:        reviewstore.New(s.Pool.GetDB()),
			Snapshots:      snapshots,
			LogsDir:        options.logsDir,
			FramesDir:      options.framesDir,
			MaxAgents:      options.maxAgents,
			MaxChildren:    options.maxChildren,
		}),
		panes:   NewPaneLiveness(options.tmux),
		emptied: emptied,
	}

	built.orchestrator = NewOrchestratorService(NewOrchestratorServiceParams{
		Agents:      s.Agents,
		Workflows:   s.Workflows,
		Transcripts: s.Transcripts,
		Harnesses:   options.harnesses,
		Launch:      built.launch,
		Lifecycle:   built.lifecycle,
		Resume:      built.resume,
		StateDir:    options.stateDir,
	})

	return built
}

// recordingSnapshots is a git that records the round refs released through it.
type recordingSnapshots struct {
	mu       sync.Mutex
	released []string
}

func (r *recordingSnapshots) ReleaseRef(_ context.Context, _, ref string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.released = append(r.released, ref)

	return nil
}

// releasedRefs is what has been released so far.
func (r *recordingSnapshots) releasedRefs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.released)
}

// onlyTheMainCheckout is git reporting one checkout of a repository, the repository itself,
// holding no branch.
type onlyTheMainCheckout struct{}

func (onlyTheMainCheckout) ListWorktrees(_ context.Context, repo string) ([]git.Worktree, error) {
	return []git.Worktree{{Dir: repo}}, nil
}

// withWorktrees rebuilds the adopt service over a worktree listing of its own.
func (a *testAgents) withWorktrees(worktrees worktreeLister) *testAgents {
	a.adopt = NewAdoptService(NewAdoptServiceParams{
		Agents:    a.adopt.agents,
		Workflows: a.adopt.workflows,
		Launch:    a.adopt.launch,
		Worktrees: worktrees,
	})

	return a
}

// fakeTmuxController is what the test services take, so a test can wrap the fake to watch
// one of its calls without every helper here having to know the wrapper's type.
type fakeTmuxController interface {
	paneController
	paneStarter
	SendKeys(target string, keys ...string) error
}

func (s *testStore) agentServiceWithLifecycle(tmux fakeTmuxController, harnesses fakeHarnesses, lifecycle agentScriptRunner) *testAgents {
	return s.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: harnesses,
		addr:      "127.0.0.1:9999",
		lifecycle: lifecycle,
	})
}

func (s *testStore) agentServiceCapped(tmux *fakeTmux, lifecycle agentScriptRunner, maxAgents int) *testAgents {
	return s.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: fakeHarnesses{harness: &fakeHarness{}},
		addr:      "127.0.0.1:9999",
		lifecycle: lifecycle,
		maxAgents: maxAgents,
	})
}

// ranHook is one hook the fake was asked to run, in the order it was asked.
type ranHook struct {
	Event  string
	Script string
	Cwd    string
}

type fakeLifecycle struct {
	dir                      string
	resumeDir                string
	provisionErr             error
	resumeErr                error
	deprovisionStartErr      error
	deprovisionRunErr        error
	deprovisionRunErrByAgent map[string]error
	holdDeprovision          bool
	held                     []func(error)
	hookErrs                 map[string]error
	hooksMu                  sync.Mutex
	hooks                    []ranHook
	provision                []string
	resumed                  []string
	deprovision              []string
	deprovisionCwd           []string
	deprovisionCtxErr        []error
}

func (f *fakeLifecycle) RunProvision(_ context.Context, _ agentstore.Agent, name string) (string, error) {
	f.provision = append(f.provision, name)
	if f.provisionErr != nil {
		return "", f.provisionErr
	}

	return f.dir, nil
}

func (f *fakeLifecycle) RunResume(_ context.Context, _ agentstore.Agent, name string) (string, error) {
	f.resumed = append(f.resumed, name)
	if f.resumeErr != nil {
		return "", f.resumeErr
	}
	if f.resumeDir != "" {
		return f.resumeDir, nil
	}

	return f.dir, nil
}

func (f *fakeLifecycle) RunHook(_ context.Context, agent agentstore.Agent, name, event string) error {
	f.hooksMu.Lock()
	defer f.hooksMu.Unlock()

	f.hooks = append(f.hooks, ranHook{
		Event:  event,
		Script: name,
		Cwd:    agent.Cwd,
	})

	return f.hookErrs[name]
}

func (f *fakeLifecycle) ranHooks() []ranHook {
	f.hooksMu.Lock()
	defer f.hooksMu.Unlock()

	return slices.Clone(f.hooks)
}

func (f *fakeLifecycle) RunDeprovision(ctx context.Context, agent agentstore.Agent, name string, done func(error)) error {
	f.deprovision = append(f.deprovision, name)
	f.deprovisionCwd = append(f.deprovisionCwd, agent.Cwd)
	f.deprovisionCtxErr = append(f.deprovisionCtxErr, ctx.Err())

	if f.deprovisionStartErr != nil {
		return f.deprovisionStartErr
	}

	if done != nil {
		runErr := f.deprovisionRunErr
		if byAgent, ok := f.deprovisionRunErrByAgent[agent.ID]; ok {
			runErr = byAgent
		}
		if f.holdDeprovision {
			f.held = append(f.held, done)

			return nil
		}
		done(runErr)
	}

	return nil
}

type workflowServiceOptions struct {
	tmux        *fakeTmux
	lifecycle   agentScriptRunner
	maxAgents   int
	maxChildren int
}

func (s *testStore) workflowService(options workflowServiceOptions) *WorkflowService {
	deleter := s.agentService(agentServiceOptions{
		tmux:      options.tmux,
		harnesses: fakeHarnesses{harness: &fakeHarness{}},
		addr:      "127.0.0.1:9999",
	})

	return NewWorkflowService(NewWorkflowServiceParams{
		Workflows:    s.Workflows,
		Agents:       s.Agents,
		AgentDeleter: deleter.lifecycle,
		Emptied:      deleter.emptied,
		Schedules:    s.Schedules,
		Tmux:         options.tmux,
		Tx:           s.Pool,
		Scripts:      options.lifecycle,
		MaxAgents:    options.maxAgents,
		MaxChildren:  options.maxChildren,
	})
}

func seed(t *testing.T, st *testStore, name, pane string, state agentstore.State) agentstore.Agent {
	t.Helper()

	return seedInWorkflow(t, st, "", name, pane, state)
}

func seedInWorkflow(t *testing.T, st *testStore, workflowID, name, pane string, state agentstore.State) agentstore.Agent {
	t.Helper()
	ctx := t.Context()

	if workflowID == "" {
		workflowID = seedWorkflow(t, st)
	}

	require.NoError(t, st.Create(ctx, agentstore.Agent{
		Name:       name,
		Pane:       pane,
		State:      state,
		WorkflowID: workflowID,
	}))

	got, err := st.Agents.GetInWorkflow(ctx, workflowID, name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return *got
}

// writingPaneFirst is the store with a pane landing on the row in the one gap an archive or a
// delete has, after it has read and killed the agents and before its own write commits.
type writingPaneFirst struct {
	*agentstore.Store
	inTheGap func()
}

func (s writingPaneFirst) Archive(ctx context.Context, ids []string) ([]agentstore.AgentPane, error) {
	s.inTheGap()

	return s.Store.Archive(ctx, ids)
}

// txWritingPaneFirst is the same gap for the writes that go through a transaction, which is
// where the delete's own removal and a workflow archive's both are.
type txWritingPaneFirst struct {
	txRunner
	inTheGap func()
}

func (t txWritingPaneFirst) InTx(ctx context.Context, fn func(store.DBTX) error) error {
	t.inTheGap()

	return t.txRunner.InTx(ctx, fn)
}

// resumeInto returns the write a resume does once its window is open, putting a pane on the
// agent's row and stamping that pane as its own.
func resumeInto(t *testing.T, st *testStore, tmux *fakeTmux, agent agentstore.Agent, pane string) func() {
	t.Helper()

	done := false

	return func() {
		if done {
			return
		}
		done = true

		agent.Pane = pane
		require.NoError(t, st.Agents.Update(t.Context(), agent))
		require.NoError(t, tmux.SetPaneOwner(pane, agent.ID))
	}
}

type paneStamper interface {
	SetPaneOwner(pane, agentID string) error
}

func seedRunning(t *testing.T, st *testStore, tmux paneStamper, name, pane string, state agentstore.State) agentstore.Agent {
	t.Helper()
	agent := seed(t, st, name, pane, state)
	require.NoError(t, tmux.SetPaneOwner(pane, agent.ID))

	return agent
}

// seedChild records an agent attached to a parent that spawned it, in that parent's workflow and
// directory.
func seedChild(t *testing.T, st *testStore, tmux paneStamper, parent agentstore.Agent) agentstore.Agent {
	t.Helper()
	ctx := t.Context()

	const name = "reviewer"

	require.NoError(t, st.Create(ctx, agentstore.Agent{
		Name:             name,
		Pane:             "%" + name,
		State:            agentstore.StateIdle,
		Cwd:              parent.Cwd,
		WorkflowID:       parent.WorkflowID,
		ParentAgentID:    &parent.ID,
		SpawnedByAgentID: &parent.ID,
		ProvisionEnabled: xptr.Ptr(false),
	}))

	got, err := st.Agents.GetInWorkflow(ctx, parent.WorkflowID, name)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NoError(t, tmux.SetPaneOwner(got.Pane, got.ID))

	return *got
}

func backdateStateSeen(t *testing.T, st *testStore, id string, age time.Duration) {
	t.Helper()
	_, err := st.Pool.GetDB().Exec(t.Context(),
		`UPDATE agents SET state_seen_at = NOW() - $2::interval WHERE id = $1`, id, age.String())
	require.NoError(t, err)
}

func stateOf(t *testing.T, st *testStore, name string) agentstore.State {
	t.Helper()
	got, err := st.Get(t.Context(), name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return got.State
}

func newManager(tmux *fakeTmux, st *testStore, flags string) *testAgents {
	service, _ := newManagerWithHarness(tmux, st, flags)

	return service
}

func workflowsFor(st *testStore, tmux *fakeTmux) *WorkflowService {
	return st.workflowService(workflowServiceOptions{tmux: tmux})
}

func newManagerWithHarness(tmux *fakeTmux, st *testStore, flags string) (*testAgents, *fakeHarness) {
	h := &fakeHarness{}

	return st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: fakeHarnesses{harness: h},
		addr:      "127.0.0.1:9999",
		flags:     flags,
	}), h
}

func agentByID(t *testing.T, st *testStore, id string) agentstore.Agent {
	t.Helper()

	all, err := st.ListAll(t.Context())
	require.NoError(t, err)

	for _, agent := range all {
		if agent.ID == id {
			return agent
		}
	}

	t.Fatalf("no agent with id %q", id)

	return agentstore.Agent{}
}

func seedWorkflow(t *testing.T, st *testStore) string {
	t.Helper()

	created, err := st.Workflows.Create(t.Context(), workflowstore.Config{Name: "workflow-" + uuid.NewString()})
	require.NoError(t, err)
	require.NotNil(t, created)

	return created.ID
}

// disableProvisioner writes the one override an agent can carry.
func disableProvisioner(t *testing.T, st *testStore, id string) {
	t.Helper()

	_, err := st.Pool.GetDB().Exec(t.Context(),
		`UPDATE agents SET provision_enabled = FALSE WHERE id = $1`, id)
	require.NoError(t, err)
}

// worktreeConfig is a workflow that provisions worktrees.
func worktreeConfig(t *testing.T, name string) workflowstore.Config {
	t.Helper()

	return workflowstore.Config{
		Name:     name,
		Cwd:      fakeCheckout(t),
		Worktree: workflowstore.WorktreeConfig{Enabled: true},
	}
}

// fakeCheckout is a directory that reads as a primary checkout.
func fakeCheckout(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o755))

	return dir
}

// mustRegistry is a registry of the harnesses a test names, which every caller here
// treats as a precondition rather than as something to handle.
func mustRegistry(t *testing.T, harnesses ...harness.Harness) *harness.Registry {
	t.Helper()
	registry, err := harness.NewRegistry(harnesses...)
	require.NoError(t, err)

	return registry
}

type fakeChannel struct {
	refuseReady bool
	refuseSend  bool
	sendErr     error
	sent        []string
	forgotten   []string
	interrupts  int
}

func (f *fakeChannel) Ready(context.Context, harness.Target) (bool, error) {
	return !f.refuseReady, nil
}

func (f *fakeChannel) Send(_ context.Context, _ harness.Target, text string) (bool, error) {
	if f.sendErr != nil {
		return false, f.sendErr
	}
	if f.refuseSend {
		return false, nil
	}
	f.sent = append(f.sent, text)

	return true, nil
}

func (f *fakeChannel) Interrupt(context.Context, harness.Target) error {
	f.interrupts++

	return nil
}

func (f *fakeChannel) Forget(agentID string) { f.forgotten = append(f.forgotten, agentID) }

type fakeWorkflowReader struct {
	workflow *workflowstore.Workflow
	archived bool
}

func (f fakeWorkflowReader) Get(ctx context.Context, id string) (*workflowstore.Workflow, error) {
	if f.archived {
		return nil, fmt.Errorf("%w: workflow %q", store.ErrNotFound, id)
	}

	return f.GetIncludingArchived(ctx, id)
}

func (f fakeWorkflowReader) GetIncludingArchived(_ context.Context, id string) (*workflowstore.Workflow, error) {
	if f.workflow != nil && f.workflow.ID == id {
		return f.workflow, nil
	}

	return nil, fmt.Errorf("%w: workflow %q", store.ErrNotFound, id)
}

func loggedContext(t *testing.T) (context.Context, func() []map[string]any) {
	t.Helper()

	var written bytes.Buffer

	ctx := logging.WithLogger(t.Context(), logging.New(&written, slog.LevelDebug))

	return ctx, func() []map[string]any {
		records := make([]map[string]any, 0, 2)
		for _, line := range strings.Split(strings.TrimSpace(written.String()), "\n") {
			if line == "" {
				continue
			}

			var record map[string]any
			require.NoError(t, json.Unmarshal([]byte(line), &record))
			records = append(records, record)
		}

		return records
	}
}

func recordFor(t *testing.T, records []map[string]any, body string) map[string]any {
	t.Helper()

	for _, record := range records {
		if record["body"] == body {
			return record
		}
	}

	require.FailNowf(t, "no record was written", "wanted %q, got %v", body, records)

	return nil
}
