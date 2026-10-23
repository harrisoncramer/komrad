package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"komrad/config"
	"komrad/db/testmodels"
	"komrad/internal/checkouts"
	"komrad/internal/frames"
	"komrad/internal/gittest"
	"komrad/internal/harness"
	"komrad/internal/harnesstest"
	"komrad/internal/logging"
	"komrad/internal/scriptrun"
	"komrad/internal/scripts"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	reviewstore "komrad/internal/store/review"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
	worktreestore "komrad/internal/store/worktree"
	"komrad/internal/storetest"
	"komrad/internal/tasks"
	"komrad/internal/tmuxtest"
	"komrad/internal/transcripts"
	"komrad/lib/clock"
	"komrad/lib/clock/clocktest"
	"komrad/lib/xptr"
	eventv1 "komrad/proto/gen/event/v1"
	"komrad/proto/gen/oneofs"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

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

func (f *fakeFrameRecorder) Forget(map[uuid.UUID]struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.forgotten++
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

// createAgent records an agent, filing it under a workflow and a task of its own when the test
// did not name one.
func (s *testStore) createAgent(ctx context.Context, agent agentstore.Agent) error {
	if agent.Harness == "" {
		agent.Harness = harnesstest.FakeID
	}
	if agent.WorkflowID == uuid.Nil {
		created, err := s.Workflows.Create(ctx, workflowstore.Config{Name: "workflow-" + uuid.NewString(), Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"}})
		if err != nil {
			return err
		}

		agent.WorkflowID = created.ID
	}
	if agent.TaskID == nil && agent.Role != agentstore.RoleOrchestrator {
		taskID, err := s.TaskFor(ctx, agent)
		if err != nil {
			return err
		}
		agent.TaskID = taskID
	}

	return s.Agents.Create(ctx, agent)
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

	_, err = s.Agents.Archive(ctx, []uuid.UUID{agent.ID})

	return err
}

func (s *testStore) GetWorkflow(ctx context.Context, id uuid.UUID) (*workflowstore.Workflow, error) {
	return s.Workflows.Get(ctx, id)
}

func (s *testStore) GetTranscript(ctx context.Context, agentID uuid.UUID) (string, error) {
	return s.Transcripts.Get(ctx, agentID)
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
	tasks        *tasks.TaskService
	orchestrator *OrchestratorService
	snapshots    *gittest.Snapshots
}

type agentServiceOptions struct {
	tmux      fakeTmuxController
	harnesses harnesstest.Registry
	addr      string
	binPath   string
	flags     string
	lifecycle agentScriptRunner
	usage     config.Usage
	logsDir   string
	framesDir string
	tx        txRunner
	stateDir  string
	sleeper   clock.Sleeper
	locks     treeLocker
}

// taskScriptRunner is what a task service needs of the scripts a test hands its agents.
type taskScriptRunner interface {
	scriptrun.HookRunner
	RunTaskDeprovision(ctx context.Context, task taskstore.Task, name string) error
}

func (s *testStore) agentService(options agentServiceOptions) *testAgents {
	tx := options.tx
	if tx == nil {
		tx = s.Pool
	}
	sleeper := options.sleeper
	if sleeper == nil {
		sleeper = &clocktest.Sleeper{}
	}
	locks := options.locks
	if locks == nil {
		locks = checkouts.NewTreeLocks()
	}

	launcher := NewLauncher(NewLauncherParams{
		Tmux:    options.tmux,
		Addr:    options.addr,
		BinPath: options.binPath,
		Usage:   options.usage,
	})
	snapshots := &gittest.Snapshots{}
	taskScripts, _ := options.lifecycle.(taskScriptRunner)
	taskService := tasks.NewTaskService(tasks.NewTaskServiceParams{
		Tasks:     s.Tasks,
		Agents:    s.Agents,
		Runs:      s.Runs,
		Workflows: s.Workflows,
		Worktrees: worktreestore.NewWorktreeStore(s.Pool.GetDB()),
		Reviews:   reviewstore.NewReviewStore(s.Pool.GetDB()),
		Snapshots: snapshots,
		Scripts:   taskScripts,
		Tx:        tx,
	})
	emptied := NewEmptyWorkflowDeleter(NewEmptyWorkflowDeleterParams{
		Workflows: s.Workflows,
		Agents:    s.Agents,
		Schedules: s.Schedules,
		Tasks:     taskService,
	})

	launch := NewLaunchService(NewLaunchServiceParams{
		Tmux:               options.tmux,
		Agents:             s.Agents,
		Workflows:          s.Workflows,
		Tasks:              taskService,
		Tx:                 tx,
		Launcher:           launcher,
		Harnesses:          options.harnesses,
		Scripts:            options.lifecycle,
		UserDefaultHarness: harnesstest.FakeID,
		UserDefaultFlags:   map[string]string{harnesstest.FakeID: options.flags},
		Usage:              options.usage,
		Sleeper:            sleeper,
	})

	built := &testAgents{
		launch:    launch,
		snapshots: snapshots,
		resume: NewResumeService(NewResumeServiceParams{
			Tmux:          options.tmux,
			Agents:        s.Agents,
			Workflows:     s.Workflows,
			Launcher:      launcher,
			LaunchService: launch,
			Harnesses:     options.harnesses,
			Scripts:       options.lifecycle,
			Locks:         locks,
			Tasks:         taskService,
			Transcripts: transcripts.NewTranscriptSyncer(transcripts.NewTranscriptSyncerParams{
				Agents:      s.Agents,
				Transcripts: s.Transcripts,
				Harnesses:   options.harnesses,
			}),
		}),
		adopt: NewAdoptService(NewAdoptServiceParams{
			Agents:    s.Agents,
			Workflows: s.Workflows,
			Launch:    launch,
			Worktrees: &gittest.Checkout{},
			Tasks:     taskService,
		}),
		lifecycle: NewLifecycleService(NewLifecycleServiceParams{
			Tmux:           options.tmux,
			Agents:         s.Agents,
			Workflows:      s.Workflows,
			EmptyWorkflows: emptied,
			Tx:             tx,
			Scripts:        options.lifecycle,
			Reviews:        reviewstore.NewReviewStore(s.Pool.GetDB()),
			Screens:        NewScreenKeeper(NewScreenKeeperParams{Panes: options.tmux, Screens: s.Screens}),
			LogsDir:        options.logsDir,
			FramesDir:      options.framesDir,
			Tasks:          taskService,
			Usage:          options.usage,
		}),
		panes:   NewPaneLiveness(options.tmux),
		emptied: emptied,
		tasks:   taskService,
	}
	built.lifecycle.agentDeleter = inlineDelete{lifecycle: built.lifecycle}

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

// withWorktrees rebuilds the adopt service over a worktree listing of its own.
func (a *testAgents) withWorktrees(worktrees checkoutReader) *testAgents {
	a.adopt = NewAdoptService(NewAdoptServiceParams{
		Agents:    a.adopt.agents,
		Workflows: a.adopt.workflows,
		Launch:    a.adopt.launch,
		Worktrees: worktrees,
		Tasks:     a.adopt.tasks,
	})

	return a
}

// fakeTmuxController is what the test services take, so a test can wrap the fake to watch
// one of its calls without every helper here having to know the wrapper's type.
type fakeTmuxController interface {
	paneController
	paneStarter
	screenCapturer
	SendKeys(target string, keys ...string) error
}

func (s *testStore) agentServiceWithLifecycle(tmux fakeTmuxController, harnesses harnesstest.Registry, lifecycle agentScriptRunner) *testAgents {
	return s.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: harnesses,
		addr:      "127.0.0.1:9999",
		lifecycle: lifecycle,
	})
}

func (s *testStore) agentServiceCapped(tmux *tmuxtest.Fake, lifecycle agentScriptRunner, maxAgents int) *testAgents {
	return s.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}},
		addr:      "127.0.0.1:9999",
		lifecycle: lifecycle,
		usage:     config.Usage{MaxAgents: maxAgents},
	})
}

// ranHook is one hook the fake was asked to run, in the order it was asked.
type ranHook struct {
	Moment eventv1.Moment
	Script string
	Cwd    string
}

type fakeLifecycle struct {
	dir                 string
	resumeDir           string
	provisionErr        error
	resumeErr           error
	deprovisionStartErr error
	hookErrs            map[string]error
	holdHooks           chan struct{}
	hooksMu             sync.Mutex
	hooks               []ranHook
	ranAs               []agentstore.Agent
	provision           []string
	resumed             []string
	deprovisionMu       sync.Mutex
	deprovision         []string
	deprovisionCwd      []string
	deprovisionCtxErr   []error
	taskDeprovisionErr  error
	holdTaskDeprovision chan struct{}
	taskDeprovisions    []string
	scriptrun.HookQueue
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

func (f *fakeLifecycle) RunHook(_ context.Context, agent agentstore.Agent, name string, moment eventv1.Moment) error {
	if f.holdHooks != nil {
		<-f.holdHooks
	}

	f.hooksMu.Lock()
	defer f.hooksMu.Unlock()

	f.hooks = append(f.hooks, ranHook{
		Moment: moment,
		Script: name,
		Cwd:    agent.Cwd,
	})
	f.ranAs = append(f.ranAs, agent)

	return f.hookErrs[name]
}

func (f *fakeLifecycle) ranHooks() []ranHook {
	f.hooksMu.Lock()
	defer f.hooksMu.Unlock()

	return slices.Clone(f.hooks)
}

// ranTaskHooks is each task hook the fake ran, as the agent it ran as.
func (f *fakeLifecycle) ranTaskHooks() []agentstore.Agent {
	f.hooksMu.Lock()
	defer f.hooksMu.Unlock()

	var ran []agentstore.Agent
	for i, hook := range f.hooks {
		if workflowstore.IsTaskMoment(hook.Moment) {
			ran = append(ran, f.ranAs[i])
		}
	}

	return ran
}

func (f *fakeLifecycle) ranDeprovisions() []string {
	f.deprovisionMu.Lock()
	defer f.deprovisionMu.Unlock()

	return slices.Clone(f.deprovision)
}

func (f *fakeLifecycle) RunDeprovision(ctx context.Context, agent agentstore.Agent, name string, done func(error)) error {
	f.deprovisionMu.Lock()
	f.deprovision = append(f.deprovision, name)
	f.deprovisionMu.Unlock()
	f.deprovisionCwd = append(f.deprovisionCwd, agent.Cwd)
	f.deprovisionCtxErr = append(f.deprovisionCtxErr, ctx.Err())

	if f.deprovisionStartErr != nil {
		return f.deprovisionStartErr
	}

	if done != nil {
		done(nil)
	}

	return nil
}

// RunTaskDeprovision records the checkout of each task it was asked to give back.
func (f *fakeLifecycle) RunTaskDeprovision(_ context.Context, task taskstore.Task, _ string) error {
	f.deprovisionMu.Lock()
	f.taskDeprovisions = append(f.taskDeprovisions, task.Checkout.Cwd)
	f.deprovisionMu.Unlock()

	if f.holdTaskDeprovision != nil {
		<-f.holdTaskDeprovision
	}

	return f.taskDeprovisionErr
}

func (f *fakeLifecycle) ranTaskDeprovisions() []string {
	f.deprovisionMu.Lock()
	defer f.deprovisionMu.Unlock()

	return slices.Clone(f.taskDeprovisions)
}

type workflowServiceOptions struct {
	tmux      *tmuxtest.Fake
	lifecycle agentScriptRunner
	hooks     workflowHookFinder
	usage     config.Usage
}

type everyHook struct{}

// FindHook finds whatever it is asked for.
func (everyHook) FindHook(name string) (Script, error) {
	return Script{Name: name}, nil
}

type knownHooks []string

// FindHook finds only the names listed.
func (k knownHooks) FindHook(name string) (Script, error) {
	if !slices.Contains(k, name) {
		return Script{}, fmt.Errorf("%w: %q is not in the hooks directory", scripts.ErrScriptNotFound, name)
	}

	return Script{Name: name}, nil
}

func (s *testStore) workflowService(options workflowServiceOptions) *WorkflowService {
	agentSvc := s.agentService(agentServiceOptions{
		tmux:      options.tmux,
		harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}},
		addr:      "127.0.0.1:9999",
		lifecycle: options.lifecycle,
		usage:     options.usage,
	})

	hooks := options.hooks
	if hooks == nil {
		hooks = everyHook{}
	}

	return NewWorkflowService(NewWorkflowServiceParams{
		Workflows:      s.Workflows,
		Agents:         s.Agents,
		AgentDeleter:   inlineDelete{lifecycle: agentSvc.lifecycle},
		EmptyWorkflows: agentSvc.emptied,
		Schedules:      s.Schedules,
		Lifecycle:      agentSvc.lifecycle,
		Tx:             s.Pool,
		Hooks:          hooks,
	})
}

type inlineDelete struct {
	lifecycle *LifecycleService
}

func (d inlineDelete) DeleteAgents(ctx context.Context, ids []uuid.UUID) error {
	claims, err := d.lifecycle.agents.MarkDeleting(ctx, ids)
	if err != nil {
		return err
	}

	for _, root := range agentstore.ClaimRoots(claims) {
		_ = d.lifecycle.DeleteAgent(ctx, root.ID, root.DeletingAt)
	}

	return nil
}

func seed(t *testing.T, st *testStore, name, pane string, state agentstore.State) agentstore.Agent {
	t.Helper()

	return seedInWorkflow(t, st, uuid.Nil, name, pane, state)
}

func seedInWorkflow(t *testing.T, st *testStore, workflowID uuid.UUID, name, pane string, state agentstore.State) agentstore.Agent {
	t.Helper()
	ctx := t.Context()

	if workflowID == uuid.Nil {
		workflowID = seedWorkflow(t, st)
	}

	require.NoError(t, st.createAgent(ctx, agentstore.Agent{
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

func seedWithRole(t *testing.T, st *testStore, workflowID uuid.UUID, name string, role agentstore.Role) agentstore.Agent {
	t.Helper()
	ctx := t.Context()

	require.NoError(t, st.createAgent(ctx, agentstore.Agent{
		Name:       name,
		Pane:       "%" + name,
		Cwd:        t.TempDir(),
		State:      agentstore.StateIdle,
		WorkflowID: workflowID,
		Role:       role,
	}))

	got, err := st.Agents.GetInWorkflow(ctx, workflowID, name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return *got
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

// buildResumeWriteParams is the agent a resume writes, the pane it lands in, and where both are
// recorded.
type buildResumeWriteParams struct {
	Store *testStore
	Tmux  *tmuxtest.Fake
	Agent agentstore.Agent
	Pane  string
}

// buildResumeWrite returns the write a resume does once its window is open, putting a pane on the
// agent's row and stamping that pane as its own.
func buildResumeWrite(t *testing.T, params buildResumeWriteParams) func() {
	t.Helper()

	done := false
	agent := params.Agent

	return func() {
		if done {
			return
		}
		done = true

		agent.Pane = params.Pane
		require.NoError(t, params.Store.Agents.Update(t.Context(), agent))
		require.NoError(t, params.Tmux.SetPaneOwner(params.Pane, agent.ID))
	}
}

type paneStamper interface {
	SetPaneOwner(pane string, agentID uuid.UUID) error
}

func seedRunning(t *testing.T, st *testStore, tmux paneStamper, name, pane string, state agentstore.State) agentstore.Agent {
	t.Helper()
	agent := seed(t, st, name, pane, state)
	require.NoError(t, tmux.SetPaneOwner(pane, agent.ID))

	return agent
}

// seedReviewer records a reviewer the writer spawned on its task, in the writer's workflow and
// directory.
func seedReviewer(t *testing.T, st *testStore, tmux paneStamper, writer agentstore.Agent) agentstore.Agent {
	t.Helper()
	ctx := t.Context()

	const name = "reviewer"

	require.NoError(t, st.createAgent(ctx, agentstore.Agent{
		Name:             name,
		Pane:             "%" + name,
		State:            agentstore.StateIdle,
		Role:             agentstore.RoleReviewer,
		Cwd:              writer.Cwd,
		WorkflowID:       writer.WorkflowID,
		TaskID:           writer.TaskID,
		SpawnedByAgentID: &writer.ID,
		ProvisionEnabled: xptr.Ptr(false),
	}))

	got, err := st.Agents.GetInWorkflow(ctx, writer.WorkflowID, name)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NoError(t, tmux.SetPaneOwner(got.Pane, got.ID))

	return *got
}

func backdateStateSeen(t *testing.T, st *testStore, id uuid.UUID, age time.Duration) {
	t.Helper()

	queries := testmodels.New(st.Pool.GetDB())
	require.NoError(t, queries.BackdateAgentStateSeen(t.Context(), testmodels.BackdateAgentStateSeenParams{
		AgeMicros: age.Microseconds(),
		ID:        id.String(),
	}))
}

func stateOf(t *testing.T, st *testStore, name string) agentstore.State {
	t.Helper()
	got, err := st.Get(t.Context(), name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return got.State
}

func newManager(tmux *tmuxtest.Fake, st *testStore) *testAgents {
	service, _ := newManagerWithHarness(tmux, st, "")

	return service
}

func newWorkflowService(st *testStore, tmux *tmuxtest.Fake) *WorkflowService {
	return st.workflowService(workflowServiceOptions{tmux: tmux})
}

func newManagerWithHarness(tmux *tmuxtest.Fake, st *testStore, flags string) (*testAgents, *harnesstest.Fake) {
	fakeHarness := &harnesstest.Fake{}

	return st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: harnesstest.Registry{Harness: fakeHarness},
		addr:      "127.0.0.1:9999",
		flags:     flags,
	}), fakeHarness
}

func agentByID(t *testing.T, st *testStore, id uuid.UUID) agentstore.Agent {
	t.Helper()

	all, err := st.Agents.ListAll(t.Context())
	require.NoError(t, err)

	for _, agent := range all {
		if agent.ID == id {
			return agent
		}
	}

	t.Fatalf("no agent with id %q", id)

	return agentstore.Agent{}
}

func seedWorkflow(t *testing.T, st *testStore) uuid.UUID {
	t.Helper()

	created, err := st.Workflows.Create(t.Context(), workflowstore.Config{Name: "workflow-" + uuid.NewString(), Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"}})
	require.NoError(t, err)
	require.NotNil(t, created)

	return created.ID
}

// worktreeConfig is a workflow that provisions worktrees.
func worktreeConfig(t *testing.T, name string) workflowstore.Config {
	t.Helper()

	return workflowstore.Config{
		Name:     name,
		Cwd:      fakeCheckout(t),
		Worktree: workflowstore.WorktreeConfig{Enabled: true, BaseBranch: "main"},
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

func checkoutOf(t *testing.T, st *testStore, agentID uuid.UUID) taskstore.Checkout {
	t.Helper()

	agent := agentByID(t, st, agentID)
	require.NotNil(t, agent.TaskID, "agent %q is on no task", agentID)
	task, err := st.Tasks.Get(t.Context(), *agent.TaskID)
	require.NoError(t, err)

	return task.Checkout
}

func (s *testStore) seedAdopted(t *testing.T, name, branch string) {
	t.Helper()

	agent, err := s.LiveAgentByName(t.Context(), name)
	require.NoError(t, err)
	require.NotNil(t, agent)
	claimed, err := s.Tasks.ClaimCheckout(t.Context(), taskstore.ClaimCheckoutParams{ID: *agent.TaskID, AdoptedBranch: branch})
	require.NoError(t, err)
	require.True(t, claimed)
}

// archiveWriters archives the named agents by archiving their tasks.
func archiveWriters(ctx context.Context, lifecycle *LifecycleService, ids []uuid.UUID) error {
	agents, err := lifecycle.agents.ListByID(ctx, ids)
	if err != nil {
		return err
	}

	for _, taskID := range agentstore.TaskIDs(agents) {
		if err := lifecycle.ArchiveTask(ctx, taskByID(taskID)); err != nil {
			return err
		}
	}

	return nil
}

// taskByID names a task by the id the store gave it.
func taskByID(id uuid.UUID) oneofs.Task {
	return oneofs.Task{TaskId: &id}
}
