package agents

import (
	"context"
	"errors"
	"testing"

	agentstore "claude-agents/internal/store/agent"
	transcriptstore "claude-agents/internal/store/transcript"
	workflowstore "claude-agents/internal/store/workflow"
	"claude-agents/internal/storetest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// fakeTmux is a scripted, recording tmux client. Reads are served from the alive
// and captures maps. Writes are appended to the recording slices so a test can
// assert what the manager sent. It implements tmuxClient and also satisfies the
// reconciler's narrower tmuxProbe.
type fakeTmux struct {
	alive     map[string]bool
	captures  map[string]string
	clientDir string

	sent           map[string][][]string
	globals        map[string]string
	newWindows     map[string]string // window name to dir
	swaps          [][2]string       // {src, dst}
	respawns       [][]string        // pane + argv per respawn
	killedPanes    []string
	detached       []string
	sessionOptions map[string]string
	// newWindowErr makes NewWindow fail, standing in for tmux refusing to open
	// a window.
	newWindowErr error
}

func newFakeTmux() *fakeTmux {
	return &fakeTmux{
		alive:          map[string]bool{},
		captures:       map[string]string{},
		sent:           map[string][][]string{},
		globals:        map[string]string{},
		newWindows:     map[string]string{},
		sessionOptions: map[string]string{},
	}
}

func (f *fakeTmux) SetGlobal(name, value string)   { f.globals[name] = value }
func (f *fakeTmux) ShowGlobal(name string) string  { return f.globals[name] }
func (f *fakeTmux) PaneAlive(pane string) bool     { return f.alive[pane] }
func (f *fakeTmux) CapturePane(pane string) string { return f.captures[pane] }
func (f *fakeTmux) ClientPath(string) string       { return f.clientDir }
func (f *fakeTmux) DetachSession(name string)      { f.detached = append(f.detached, name) }

func (f *fakeTmux) SetSessionOption(target, name, value string) {
	f.sessionOptions[target+"/"+name] = value
}

func (f *fakeTmux) SendKeys(target string, keys ...string) error {
	if target == "" {
		return errors.New("refusing to send keys to an empty target")
	}
	f.sent[target] = append(f.sent[target], keys)

	return nil
}

// CurrentPane returns a fixed nav pane id for the panel's own pane.
func (f *fakeTmux) CurrentPane() (string, error) { return "%nav", nil }

// SplitRight records the anchor and returns a fixed stage pane id.
func (f *fakeTmux) SplitRight(anchorPane, dir string) string { return "%stage" }

// NewWindow records the window and returns a synthetic pane id, marking it alive
// so pane-keyed liveness sees it. Setting newWindowErr makes it fail, standing in
// for tmux refusing a name it cannot use.
func (f *fakeTmux) NewWindow(session, name, dir string) (string, error) {
	if f.newWindowErr != nil {
		return "", f.newWindowErr
	}

	f.newWindows[name] = dir
	pane := "%" + name
	f.alive[pane] = true

	return pane, nil
}

func (f *fakeTmux) SwapPanes(src, dst string) { f.swaps = append(f.swaps, [2]string{src, dst}) }

func (f *fakeTmux) RespawnPane(pane, dir string, argv ...string) {
	f.respawns = append(f.respawns, append([]string{pane}, argv...))
}

func (f *fakeTmux) KillPane(pane string) {
	f.killedPanes = append(f.killedPanes, pane)
	delete(f.alive, pane)
}

// lastKeys returns the most recent SendKeys payload sent to target.
func (f *fakeTmux) lastKeys(target string) []string {
	sends := f.sent[target]
	if len(sends) == 0 {
		return nil
	}

	return sends[len(sends)-1]
}

// testStore gathers the domain stores a test needs behind one handle, built over
// one pool. Nothing in production holds this, since each service is still constructed
// with only the store it depends on. It exists so a test can arrange rows across
// domains without threading three handles through every helper.
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
	return s.Agents.Get(ctx, name)
}

// Upsert records an agent, filing it under a workflow of its own when the test
// did not name one. Every agent belongs to a workflow, and most tests here are
// about something else entirely.
func (s *testStore) Upsert(ctx context.Context, agent agentstore.Agent) error {
	if agent.WorkflowID == "" {
		created, err := s.Workflows.Create(ctx, workflowstore.Config{Name: "workflow-" + uuid.NewString()})
		if err != nil {
			return err
		}

		agent.WorkflowID = created.ID
	}

	return s.Agents.Upsert(ctx, agent)
}

func (s *testStore) List(ctx context.Context) ([]agentstore.Agent, error) {
	return s.Agents.List(ctx)
}

func (s *testStore) ListAll(ctx context.Context) ([]agentstore.Agent, error) {
	return s.Agents.ListAll(ctx)
}

func (s *testStore) SoftDelete(ctx context.Context, name string) error {
	return s.Agents.Archive(ctx, name)
}

func (s *testStore) GetWorkflow(ctx context.Context, id string) (*workflowstore.Workflow, error) {
	return s.Workflows.Get(ctx, id)
}

func (s *testStore) GetTranscript(ctx context.Context, agentID string) (transcriptstore.Transcript, error) {
	return s.Transcripts.Get(ctx, agentID)
}

// agentService builds the service under test over these stores, the same way
// the composition root does.
func (s *testStore) agentService(tmux agentTmux, launcher *Launcher, flags string) *AgentService {
	return NewAgentService(AgentParams{
		Tmux:        tmux,
		Agents:      s.Agents,
		Workflows:   s.Workflows,
		Tx:          s.Pool,
		Launcher:    launcher,
		ClaudeFlags: flags,
	})
}

// workflowService builds the workflow service over these stores.
func (s *testStore) workflowService() *WorkflowService {
	return NewWorkflowService(WorkflowParams{Workflows: s.Workflows, Schedules: s.Schedules, Tx: s.Pool})
}

// seed inserts an agent in a given state and returns it with its generated id.
func seed(t *testing.T, st *testStore, name, pane, state string) agentstore.Agent {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, st.Upsert(ctx, agentstore.Agent{Name: name, Pane: pane, State: state}))

	got, err := st.Get(ctx, name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return *got
}

func stateOf(t *testing.T, st *testStore, name string) string {
	t.Helper()
	got, err := st.Get(context.Background(), name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return got.State
}

// newManager builds an agent service over the fake tmux and a real store, which
// is what most of these tests drive.
func newManager(tmux *fakeTmux, st *testStore, flags string) *AgentService {
	return newAgentService(tmux, st, flags, "")
}

// workflowsFor builds the workflow service over the same store, for the tests
// that set an agent's workflow up before acting on the agent.
func workflowsFor(st *testStore) *WorkflowService {
	return st.workflowService()
}

// newAgentService is the same, with the launcher pointed at a specific address so a
// test can assert on what a spawned agent is told.
func newAgentService(tmux *fakeTmux, st *testStore, flags, addr string) *AgentService {
	return st.agentService(tmux, NewLauncher(tmux, addr), flags)
}

// agentByID finds an agent among every row, archived ones included, since a test
// that restores one holds its id rather than its current name.
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

// seedWorkflow creates a workflow and returns its id. Every agent belongs to
// one, so a test launching an agent needs somewhere to put it even when the
// workflow itself is beside the point.
func seedWorkflow(t *testing.T, st *testStore) string {
	t.Helper()

	created, err := st.Workflows.Create(t.Context(), workflowstore.Config{Name: "workflow-" + uuid.NewString()})
	require.NoError(t, err)
	require.NotNil(t, created)

	return created.ID
}
