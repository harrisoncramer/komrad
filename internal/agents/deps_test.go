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
	"komrad/internal/frames"
	"komrad/internal/gittest"
	"komrad/internal/harness"
	"komrad/internal/harnesstest"
	"komrad/internal/logging"
	"komrad/internal/scriptrun"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	reviewstore "komrad/internal/store/review"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/storetest"
	"komrad/internal/tmuxtest"
	"komrad/lib/xptr"

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

func (f *fakeFrameRecorder) Forget(map[string]struct{}) {
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

// createAgent records an agent, filing it under a workflow of its own when the test
// did not name one.
func (s *testStore) createAgent(ctx context.Context, agent agentstore.Agent) error {
	if agent.Harness == "" {
		agent.Harness = harnesstest.FakeID
	}
	if agent.WorkflowID == "" {
		created, err := s.Workflows.Create(ctx, workflowstore.Config{Name: "workflow-" + uuid.NewString(), Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"}})
		if err != nil {
			return err
		}

		agent.WorkflowID = created.ID
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
	snapshots    *gittest.Snapshots
}

type agentServiceOptions struct {
	tmux      fakeTmuxController
	harnesses harnesstest.Registry
	addr      string
	flags     string
	lifecycle agentScriptRunner
	usage     config.Usage
	logsDir   string
	framesDir string
	agents    testAgentStore
	tx        txRunner
	stateDir  string
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

	launcher := NewLauncher(NewLauncherParams{Tmux: options.tmux, Addr: options.addr, Usage: options.usage})
	emptied := NewEmptyWorkflowDeleter(NewEmptyWorkflowDeleterParams{
		Workflows: s.Workflows,
		Agents:    s.Agents,
		Schedules: s.Schedules,
	})

	launch := NewLaunchService(NewLaunchServiceParams{
		Tmux:               options.tmux,
		Agents:             agents,
		Reviews:            reviewstore.NewReviewStore(s.Pool.GetDB()),
		Workflows:          s.Workflows,
		Tx:                 tx,
		Launcher:           launcher,
		Harnesses:          options.harnesses,
		Scripts:            options.lifecycle,
		UserDefaultHarness: harnesstest.FakeID,
		UserDefaultFlags:   map[string]string{harnesstest.FakeID: options.flags},
		Usage:              options.usage,
	})

	snapshots := &gittest.Snapshots{}

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
			Worktrees: &gittest.Checkout{},
		}),
		lifecycle: NewLifecycleService(NewLifecycleServiceParams{
			Tmux:           options.tmux,
			Agents:         agents,
			Workflows:      s.Workflows,
			EmptyWorkflows: emptied,
			Tx:             tx,
			Scripts:        options.lifecycle,
			Reviews:        reviewstore.NewReviewStore(s.Pool.GetDB()),
			Snapshots:      snapshots,
			LogsDir:        options.logsDir,
			FramesDir:      options.framesDir,
			Usage:          options.usage,
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
	hookErrs                 map[string]error
	holdHooks                chan struct{}
	hooksMu                  sync.Mutex
	hooks                    []ranHook
	provision                []string
	resumed                  []string
	deprovision              []string
	deprovisionCwd           []string
	deprovisionCtxErr        []error
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

func (f *fakeLifecycle) RunHook(_ context.Context, agent agentstore.Agent, name, event string) error {
	if f.holdHooks != nil {
		<-f.holdHooks
	}

	f.hooksMu.Lock()
	defer f.hooksMu.Unlock()

	f.hooks = append(f.hooks, ranHook{
		Event:  event,
		Script: name,
		Cwd:    agent.Cwd,
	})

	return f.hookErrs[name]
}

func (f *fakeLifecycle) RunPostDeleteHook(ctx context.Context, agent agentstore.Agent, name, event string) error {
	return f.RunHook(ctx, agent, name, event)
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
		done(runErr)
	}

	return nil
}

type workflowServiceOptions struct {
	tmux      *tmuxtest.Fake
	lifecycle agentScriptRunner
	usage     config.Usage
}

func (s *testStore) workflowService(options workflowServiceOptions) *WorkflowService {
	deleter := s.agentService(agentServiceOptions{
		tmux:      options.tmux,
		harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}},
		addr:      "127.0.0.1:9999",
	})

	return NewWorkflowService(NewWorkflowServiceParams{
		Workflows:    s.Workflows,
		Agents:       s.Agents,
		AgentDeleter: inlineDelete{lifecycle: deleter.lifecycle},
		Emptied:      deleter.emptied,
		Schedules:    s.Schedules,
		Tmux:         options.tmux,
		Tx:           s.Pool,
		Scripts:      options.lifecycle,
		Usage:        options.usage,
	})
}

type deletingClaimer interface {
	MarkDeleting(ctx context.Context, ids []string) ([]agentstore.Claim, error)
}

// deleteNow claims agents and runs the delete of each root the claim took, as the queued jobs do.
func (s *LifecycleService) deleteNow(ctx context.Context, ids []string) error {
	claimer, ok := s.agents.(deletingClaimer)
	if !ok {
		return fmt.Errorf("the agent store %T cannot claim a delete", s.agents)
	}

	claims, err := claimer.MarkDeleting(ctx, ids)
	if err != nil {
		return err
	}

	for _, id := range agentstore.ClaimRoots(claims) {
		if err := s.DeleteAgent(ctx, id); err != nil {
			return err
		}
	}

	return nil
}

type inlineDelete struct {
	lifecycle *LifecycleService
}

func (d inlineDelete) DeleteAgents(ctx context.Context, ids []string) error {
	return d.lifecycle.deleteNow(ctx, ids)
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

	require.NoError(t, st.createAgent(ctx, agentstore.Agent{
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

	queries := testmodels.New(st.Pool.GetDB())
	require.NoError(t, queries.BackdateAgentStateSeen(t.Context(), testmodels.BackdateAgentStateSeenParams{
		AgeMicros: age.Microseconds(),
		ID:        id,
	}))
}

func stateOf(t *testing.T, st *testStore, name string) agentstore.State {
	t.Helper()
	got, err := st.Get(t.Context(), name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return got.State
}

func newManager(tmux *tmuxtest.Fake, st *testStore, flags string) *testAgents {
	service, _ := newManagerWithHarness(tmux, st, flags)

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

func agentByID(t *testing.T, st *testStore, id string) agentstore.Agent {
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

func seedWorkflow(t *testing.T, st *testStore) string {
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
