package agents

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"weave/internal/harness"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	transcriptstore "weave/internal/store/transcript"
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
	swaps          [][2]string
	respawns       [][]string
	killedPanes    []string
	detached       []string
	sessionOptions map[string]string
	newWindowErr   error
	setOwnerErr    error
	sendKeysErr    error
	paneOwnersErr  error
	clearOwnerErr  error
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
	}
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

func (f *fakeTmux) GetClientPath(string) string {
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

func (f *fakeTmux) ClearPaneOwner(pane string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.clearOwnerErr != nil {
		return f.clearOwnerErr
	}
	delete(f.owners, pane)

	return nil
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

func (f *fakeTmux) SplitRight(anchorPane, dir string) string { return "%stage" }

func (f *fakeTmux) NewWindow(session, name, dir string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.newWindowErr != nil {
		return "", f.newWindowErr
	}

	f.newWindows[name] = dir
	pane := "%" + name
	f.owners[pane] = ""

	return pane, nil
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

func (f *fakeTmux) SwapPanes(src, dst string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.swaps = append(f.swaps, [2]string{src, dst})
}

func (f *fakeTmux) RespawnPane(pane, dir string, argv ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.respawns = append(f.respawns, append([]string{pane}, argv...))
}

func (f *fakeTmux) KillPane(pane string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.killedPanes = append(f.killedPanes, pane)
	delete(f.owners, pane)
}

func (f *fakeTmux) lastKeys(target string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	sends := f.sent[target]
	if len(sends) == 0 {
		return nil
	}

	return sends[len(sends)-1]
}

const (
	busyFrame         = "frame: working"
	idleFrame         = "frame: idle"
	composingFrame    = "frame: composing"
	waitFrame         = "frame: waiting"
	unrecognizedFrame = "frame: unrecognized"
)

const testHarnessID = "claude"

type fakeHarness struct {
	mu         sync.Mutex
	launches   []harness.Launch
	commandErr error
	found      string
}

func (f *fakeHarness) GetID() string     { return testHarnessID }
func (f *fakeHarness) GetBinary() string { return testHarnessID }

func (f *fakeHarness) BuildCommand(launch harness.Launch) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.launches = append(f.launches, launch)
	if f.commandErr != nil {
		return "", f.commandErr
	}

	return "agent-cli --session " + launch.SessionID, nil
}

func (f *fakeHarness) MintSessionID() string { return uuid.NewString() }

func (f *fakeHarness) GetInterruptKeys() []string { return []string{"Escape"} }

func (f *fakeHarness) ReadActivity(frame string) harness.Activity {
	switch {
	case frame == busyFrame:
		return harness.ActivityWorking
	case frame == waitFrame:
		return harness.ActivityWaiting
	case frame == composingFrame:
		return harness.ActivityComposing
	case frame == "":
		return harness.ActivityUnknown
	case strings.HasPrefix(frame, unrecognizedFrame):
		return harness.ActivityUnrecognized
	default:
		return harness.ActivityIdle
	}
}

func (f *fakeHarness) GetLogPath(string) string { return "" }

func (f *fakeHarness) FindSession(string) (string, error) {
	return f.found, nil
}

func (f *fakeHarness) ReadLog(string, int64) (harness.LogRead, error) {
	return harness.LogRead{}, nil
}

type fakeHarnesses struct {
	harness *fakeHarness
	only    []string
}

func (f fakeHarnesses) GetByID(id string) (harness.Harness, error) {
	if len(f.only) > 0 && !slices.Contains(f.only, id) {
		return nil, fmt.Errorf("%w: %q", harness.ErrNotRegistered, id)
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

func (s *testStore) Close() error {
	return s.Pool.Close()
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

// ForgetPane drops an agent's recorded pane, which is what the reconciler does to a row
// whose pane is no longer its own.
func (s *testStore) ForgetPane(ctx context.Context, params agentstore.ForgetPaneParams) (bool, error) {
	return s.Agents.ForgetPane(ctx, params)
}

func (s *testStore) List(ctx context.Context) ([]agentstore.Agent, error) {
	return s.Agents.List(ctx)
}

func (s *testStore) ListAll(ctx context.Context) ([]agentstore.Agent, error) {
	return s.Agents.ListAll(ctx)
}

// SoftDelete archives the live agent with this name, which is how these tests reach the
// archive without holding an id.
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

func (s *testStore) GetTranscript(ctx context.Context, agentID string) (transcriptstore.Transcript, error) {
	return s.Transcripts.Get(ctx, agentID)
}

// testAgentStore is the one store these tests hand to all four services at once, and no
// production type is expected to satisfy the whole of it, since each service asks only for its
// own slice.
type testAgentStore interface {
	launchAgentStore
	resumeAgentStore
	adoptAgentStore
	lifecycleAgentStore
}

// testAgents is what a test drives instead of one service, holding the pieces an AgentService
// used to be.
type testAgents struct {
	launch    *LaunchService
	resume    *ResumeService
	adopt     *AdoptService
	lifecycle *LifecycleService
	panes     *PaneLiveness
	emptied   *EmptyWorkflowDeleter
}

type agentServiceOptions struct {
	tmux        fakeTmuxController
	harnesses   fakeHarnesses
	addr        string
	flags       string
	lifecycle   agentActions
	maxAgents   int
	maxChildren int
	notifier    systemMailSender
	logsDir     string
	agents      testAgentStore
	tx          txRunner
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

	launcher := NewLauncher(LauncherParams{Tmux: options.tmux, Addr: options.addr})
	emptied := NewEmptyWorkflowDeleter(EmptyWorkflowDeleterParams{
		Workflows: s.Workflows,
		Agents:    s.Agents,
		Schedules: s.Schedules,
	})

	launch := NewLaunchService(LaunchServiceParams{
		Tmux:               options.tmux,
		Agents:             agents,
		Workflows:          s.Workflows,
		Tx:                 tx,
		Launcher:           launcher,
		Harnesses:          options.harnesses,
		Actions:            options.lifecycle,
		UserDefaultHarness: testHarnessID,
		UserDefaultFlags:   map[string]string{testHarnessID: options.flags},
		MaxAgents:          options.maxAgents,
		MaxChildren:        options.maxChildren,
	})

	return &testAgents{
		launch: launch,
		resume: NewResumeService(ResumeServiceParams{
			Tmux:      options.tmux,
			Agents:    agents,
			Workflows: s.Workflows,
			Launcher:  launcher,
			Harnesses: options.harnesses,
			Actions:   options.lifecycle,
		}),
		adopt: NewAdoptService(AdoptServiceParams{
			Agents:    agents,
			Workflows: s.Workflows,
			Launch:    launch,
		}),
		lifecycle: NewLifecycleService(LifecycleServiceParams{
			Tmux:           options.tmux,
			Agents:         agents,
			Workflows:      s.Workflows,
			EmptyWorkflows: emptied,
			Tx:             tx,
			Actions:        options.lifecycle,
			Notifier:       options.notifier,
			LogsDir:        options.logsDir,
			MaxAgents:      options.maxAgents,
		}),
		panes:   NewPaneLiveness(options.tmux),
		emptied: emptied,
	}
}

// withPullRequests rebuilds the adopt service over a resolver, which is what a test adopting
// by pull request rather than by branch needs.
func (a *testAgents) withPullRequests(resolver pullRequestResolver) *testAgents {
	a.adopt = NewAdoptService(AdoptServiceParams{
		Agents:       a.adopt.agents,
		Workflows:    a.adopt.workflows,
		Launch:       a.adopt.launch,
		PullRequests: resolver,
	})

	return a
}

// fakeTmuxController is what the test services take, so a test can wrap the fake to watch
// one of its calls without every helper here having to know the wrapper's type.
type fakeTmuxController interface {
	paneController
	paneWriter
}

func (s *testStore) agentServiceWithLifecycle(tmux fakeTmuxController, harnesses fakeHarnesses, lifecycle agentActions) *testAgents {
	return s.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: harnesses,
		addr:      "127.0.0.1:9999",
		lifecycle: lifecycle,
	})
}

func (s *testStore) agentServiceCapped(tmux *fakeTmux, lifecycle agentActions, maxAgents int) *testAgents {
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
	Action string
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
	hookErrs                 map[string]error
	hooksMu                  sync.Mutex
	hooks                    []ranHook
	provision                []string
	provisionPane            []string
	resumed                  []string
	deprovision              []string
	deprovisionCwd           []string
	deprovisionCtxErr        []error
}

func (f *fakeLifecycle) RunProvisionAction(_ context.Context, _ agentstore.Agent, name, pane string) (string, error) {
	f.provision = append(f.provision, name)
	f.provisionPane = append(f.provisionPane, pane)
	if f.provisionErr != nil {
		return "", f.provisionErr
	}

	return f.dir, nil
}

func (f *fakeLifecycle) RunResumeAction(_ context.Context, _ agentstore.Agent, name string) (string, error) {
	f.resumed = append(f.resumed, name)
	if f.resumeErr != nil {
		return "", f.resumeErr
	}
	if f.resumeDir != "" {
		return f.resumeDir, nil
	}

	return f.dir, nil
}

func (f *fakeLifecycle) RunHookAction(_ context.Context, agent agentstore.Agent, name, event string) error {
	f.hooksMu.Lock()
	defer f.hooksMu.Unlock()

	f.hooks = append(f.hooks, ranHook{Event: event, Action: name, Cwd: agent.Cwd})

	return f.hookErrs[name]
}

func (f *fakeLifecycle) ranHooks() []ranHook {
	f.hooksMu.Lock()
	defer f.hooksMu.Unlock()

	return slices.Clone(f.hooks)
}

func (f *fakeLifecycle) RunDeprovisionAction(ctx context.Context, agent agentstore.Agent, name string, done func(error)) error {
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
		done(runErr)
	}

	return nil
}

func (s *testStore) workflowService(tmux *fakeTmux) *WorkflowService {
	return s.workflowServiceCapped(tmux, 0)
}

func (s *testStore) workflowServiceCapped(tmux *fakeTmux, maxAgents int) *WorkflowService {
	return s.workflowServiceWithNotifier(tmux, maxAgents, nil)
}

func (s *testStore) workflowServiceWithNotifier(tmux *fakeTmux, maxAgents int, notifier systemMailSender) *WorkflowService {
	deleter := s.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: fakeHarnesses{harness: &fakeHarness{}},
		addr:      "127.0.0.1:9999",
	})

	return NewWorkflowService(WorkflowParams{
		Workflows:    s.Workflows,
		Agents:       s.Agents,
		AgentDeleter: deleter.lifecycle,
		Emptied:      deleter.emptied,
		Schedules:    s.Schedules,
		Tmux:         tmux,
		Tx:           s.Pool,
		MaxAgents:    maxAgents,
		Notifier:     notifier,
	})
}

func seed(t *testing.T, st *testStore, name, pane, state string) agentstore.Agent {
	t.Helper()

	return seedInWorkflow(t, st, "", name, pane, state)
}

func seedInWorkflow(t *testing.T, st *testStore, workflowID, name, pane, state string) agentstore.Agent {
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

func seedRunning(t *testing.T, st *testStore, tmux paneStamper, name, pane, state string) agentstore.Agent {
	t.Helper()
	agent := seed(t, st, name, pane, state)
	require.NoError(t, tmux.SetPaneOwner(pane, agent.ID))

	return agent
}

// seedChild records an agent attached to a parent that spawned it, which is the ordinary case:
// an agent asks for a child of its own.
func seedChild(t *testing.T, st *testStore, tmux paneStamper, parent agentstore.Agent) agentstore.Agent {
	t.Helper()

	return seedChildSpawnedBy(t, st, tmux, parent, parent.ID)
}

// seedChildSpawnedBy records an agent attached to a parent, in that parent's workflow and
// directory, under whichever agent asked for it.
func seedChildSpawnedBy(t *testing.T, st *testStore, tmux paneStamper, parent agentstore.Agent, spawnerID string) agentstore.Agent {
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
		SpawnedByAgentID: &spawnerID,
		ProvisionEnabled: xptr.Ptr(false),
	}))

	got, err := st.Agents.GetInWorkflow(ctx, parent.WorkflowID, name)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NoError(t, tmux.SetPaneOwner(got.Pane, got.ID))

	return *got
}

func seedSpawned(t *testing.T, st *testStore, parentID string) agentstore.Agent {
	t.Helper()
	ctx := t.Context()
	workflowID := seedWorkflow(t, st)

	require.NoError(t, st.Create(ctx, agentstore.Agent{
		Name:             "child",
		Pane:             "%child",
		State:            agentstore.StateThinking,
		WorkflowID:       workflowID,
		SpawnedByAgentID: &parentID,
	}))

	got, err := st.Agents.GetInWorkflow(ctx, workflowID, "child")
	require.NoError(t, err)
	require.NotNil(t, got)

	return *got
}

func backdateStateSeen(t *testing.T, st *testStore, id string, age time.Duration) {
	t.Helper()
	_, err := st.Pool.GetDB().Exec(t.Context(),
		`UPDATE agents SET state_seen_at = NOW() - $2::interval WHERE id = $1`, id, age.String())
	require.NoError(t, err)
}

func stateOf(t *testing.T, st *testStore, name string) string {
	t.Helper()
	got, err := st.Get(context.Background(), name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return got.State
}

func newManager(tmux *fakeTmux, st *testStore, flags string) *testAgents {
	service, _ := newManagerWithHarness(tmux, st, flags)

	return service
}

func workflowsFor(st *testStore, tmux *fakeTmux) *WorkflowService {
	return st.workflowService(tmux)
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

func backdateNudge(t *testing.T, st *testStore, id string, age time.Duration) {
	t.Helper()
	_, err := st.Pool.GetDB().Exec(t.Context(),
		`UPDATE agents SET nudged_at = NOW() - $2::interval WHERE id = $1`, id, age.String())
	require.NoError(t, err)
}

func backdateStateChanged(t *testing.T, st *testStore, id string, age time.Duration) {
	t.Helper()
	_, err := st.Pool.GetDB().Exec(t.Context(),
		`UPDATE agents SET state_changed_at = NOW() - $2::interval WHERE id = $1`, id, age.String())
	require.NoError(t, err)
}

// worktreeConfig is a workflow that provisions worktrees, which is the only way a workflow has
// anything to tear down now that it cannot name a script of its own.
func worktreeConfig(t *testing.T, name string) workflowstore.Config {
	t.Helper()

	return workflowstore.Config{
		Name:     name,
		Cwd:      initRepo(t),
		Worktree: workflowstore.WorktreeConfig{Enabled: true},
	}
}
