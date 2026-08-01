package agents

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"weave/internal/harness"
	agentstore "weave/internal/store/agent"
	transcriptstore "weave/internal/store/transcript"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/storetest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// fakeTmux is a scripted, recording tmux client. Reads are served from the owners
// and captures maps. Writes are appended to the recording slices so a test can
// assert what the manager sent. It implements tmuxClient and also satisfies the
// reconciler's narrower tmuxProbe.
type fakeTmux struct {
	// owners maps a live pane to the agent stamped on it, which is how the real
	// client answers whether a pane is still a given agent's. A pane present here
	// exists; one absent is either gone or was never opened.
	owners    map[string]string
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
	// setOwnerErr makes SetPaneOwner fail, standing in for tmux refusing to stamp
	// the pane a launch was about to run in.
	setOwnerErr error
	// sendKeysErr makes SendKeys fail, standing in for a launch that never reaches
	// the pane's shell.
	sendKeysErr error
}

func newFakeTmux() *fakeTmux {
	return &fakeTmux{
		owners:         map[string]string{},
		captures:       map[string]string{},
		sent:           map[string][][]string{},
		globals:        map[string]string{},
		newWindows:     map[string]string{},
		sessionOptions: map[string]string{},
	}
}

func (f *fakeTmux) SetGlobal(name, value string)   { f.globals[name] = value }
func (f *fakeTmux) ShowGlobal(name string) string  { return f.globals[name] }
func (f *fakeTmux) PaneOwner(pane string) string   { return f.owners[pane] }
func (f *fakeTmux) CapturePane(pane string) string { return f.captures[pane] }
func (f *fakeTmux) ClientPath(string) string       { return f.clientDir }
func (f *fakeTmux) DetachSession(name string)      { f.detached = append(f.detached, name) }

func (f *fakeTmux) SetSessionOption(target, name, value string) {
	f.sessionOptions[target+"/"+name] = value
}

func (f *fakeTmux) SendKeys(target string, keys ...string) error {
	if f.sendKeysErr != nil {
		return f.sendKeysErr
	}
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

// NewWindow records the window and returns a synthetic pane id. The pane is not
// owned by anybody until it is stamped, which is what the service does next.
// Setting newWindowErr makes it fail, standing in for tmux refusing a name it
// cannot use.
func (f *fakeTmux) NewWindow(session, name, dir string) (string, error) {
	if f.newWindowErr != nil {
		return "", f.newWindowErr
	}

	f.newWindows[name] = dir
	pane := "%" + name
	f.owners[pane] = ""

	return pane, nil
}

// SetPaneOwner stamps a pane with an agent id, which is what makes the pane answer
// to that agent. Setting setOwnerErr makes it fail.
func (f *fakeTmux) SetPaneOwner(pane, agentID string) error {
	if f.setOwnerErr != nil {
		return f.setOwnerErr
	}

	f.owners[pane] = agentID

	return nil
}

func (f *fakeTmux) SwapPanes(src, dst string) { f.swaps = append(f.swaps, [2]string{src, dst}) }

func (f *fakeTmux) RespawnPane(pane, dir string, argv ...string) {
	f.respawns = append(f.respawns, append([]string{pane}, argv...))
}

func (f *fakeTmux) KillPane(pane string) {
	f.killedPanes = append(f.killedPanes, pane)
	delete(f.owners, pane)
}

// lastKeys returns the most recent SendKeys payload sent to target.
func (f *fakeTmux) lastKeys(target string) []string {
	sends := f.sent[target]
	if len(sends) == 0 {
		return nil
	}

	return sends[len(sends)-1]
}

// The frames these tests hand the reconciler are labels rather than any CLI's
// chrome. Which state a real frame implies is the harness's own question, tested
// against real captures where the detectors live, so nothing here has to be
// reshaped when a CLI redraws its UI.
const (
	busyFrame = "frame: working"
	idleFrame = "frame: idle"
	waitFrame = "frame: waiting"
)

// testHarnessID is the harness every seeded agent names, and what the fake registry
// is asked for. It is claude's own id so the tests that drive the real harness and
// the tests that drive the fake seed their agents the same way.
const testHarnessID = "claude"

// fakeHarness stands in for an agent CLI in the tests that are about the panel's
// own behaviour rather than any one CLI's. It records what it was asked for, so a
// test asserts on what the panel composed rather than on how a CLI would spell it.
type fakeHarness struct {
	// launches records every command the harness was asked to build.
	launches []harness.Launch
	// relocated records every conversation it was asked to carry, and to where.
	relocated [][2]string
	// commandErr makes building the command line fail, standing in for a harness
	// that cannot assemble one.
	commandErr error
	// found is the conversation FindSession reports, for the adoption path a harness
	// that names its own sessions goes through.
	found string
	// reported is what the CLI says about a conversation, keyed by session id, standing
	// in for a harness that reads more than the pane. An entry here beats the frame.
	reported map[string]harness.Activity
}

func (f *fakeHarness) GetID() string { return testHarnessID }

func (f *fakeHarness) BuildCommand(launch harness.Launch) (string, error) {
	f.launches = append(f.launches, launch)
	if f.commandErr != nil {
		return "", f.commandErr
	}

	return "agent-cli --session " + launch.SessionID, nil
}

func (f *fakeHarness) MintSessionID() string { return uuid.NewString() }

func (f *fakeHarness) ReadActivity(obs harness.Observation) harness.Reading {
	if activity, ok := f.reported[obs.SessionID]; ok {
		return harness.Reading{Activity: activity, Reported: true}
	}

	switch obs.Frame {
	case busyFrame:
		return harness.Reading{Activity: harness.ActivityWorking}
	case waitFrame:
		return harness.Reading{Activity: harness.ActivityWaiting}
	case "":
		return harness.Reading{Activity: harness.ActivityUnknown}
	default:
		return harness.Reading{Activity: harness.ActivityIdle}
	}
}

func (f *fakeHarness) Relocate(sessionID, dir string) error {
	f.relocated = append(f.relocated, [2]string{sessionID, dir})

	return nil
}

// The log half is here to satisfy the interface a registry hands back. The syncer's
// own tests drive a real harness over a real log, since the format is the point
// there.
func (f *fakeHarness) GetLogPath(string) string { return "" }

// found is what FindSession answers with, for a test about adopting the id a harness
// named itself. Empty means this fake pre-mints like claude does.
func (f *fakeHarness) FindSession(string) (string, error) {
	return f.found, nil
}

func (f *fakeHarness) ReadLog(string, int64) (harness.LogRead, error) {
	return harness.LogRead{}, nil
}

// fakeHarnesses is a registry over one fake harness. It answers for every id, since
// most tests here are not about which CLI an agent named; a test that is about that
// sets only, and every other id then resolves to ErrNotRegistered.
type fakeHarnesses struct {
	harness *fakeHarness
	only    []string
}

func (f fakeHarnesses) Resolve(id string) (harness.Harness, error) {
	if len(f.only) > 0 && !slices.Contains(f.only, id) {
		return nil, fmt.Errorf("%w: %q", harness.ErrNotRegistered, id)
	}

	return f.harness, nil
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

// Create records an agent, filing it under a workflow of its own when the test
// did not name one. Every agent belongs to a workflow, and most tests here are
// about something else entirely.
func (s *testStore) Create(ctx context.Context, agent agentstore.Agent) error {
	// The column is explicit on insert, so a test that did not care which CLI its
	// agent runs would otherwise store an empty id no registry resolves.
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

// ClearPane drops an agent's recorded pane, which is what the reconciler does to
// a row whose pane is no longer its own.
func (s *testStore) ClearPane(ctx context.Context, id string) error {
	return s.Agents.ClearPane(ctx, id)
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
func (s *testStore) agentService(tmux *fakeTmux, harnesses fakeHarnesses, addr, flags string) *AgentService {
	return NewAgentService(AgentParams{
		Tmux:           tmux,
		Agents:         s.Agents,
		Workflows:      s.Workflows,
		Tx:             s.Pool,
		Launcher:       NewLauncher(tmux, addr),
		Harnesses:      harnesses,
		DefaultHarness: testHarnessID,
		DefaultFlags:   map[string]string{testHarnessID: flags},
	})
}

// agentServiceWithLifecycle builds the service with a stand-in for the actions a
// workflow runs around its agents' lives, which the plain builder leaves absent the way
// a panel with no actions directory does.
func (s *testStore) agentServiceWithLifecycle(tmux *fakeTmux, harnesses fakeHarnesses, lifecycle lifecycleActions) *AgentService {
	return NewAgentService(AgentParams{
		Tmux:           tmux,
		Agents:         s.Agents,
		Workflows:      s.Workflows,
		Tx:             s.Pool,
		Launcher:       NewLauncher(tmux, "127.0.0.1:9999"),
		Harnesses:      harnesses,
		Lifecycle:      lifecycle,
		DefaultHarness: testHarnessID,
	})
}

// agentServiceCapped builds the service with a ceiling and the workflow actions wired,
// since what a refused create must not do is run the setup action.
func (s *testStore) agentServiceCapped(tmux *fakeTmux, lifecycle lifecycleActions, maxAgents int) *AgentService {
	return NewAgentService(AgentParams{
		Tmux:           tmux,
		Agents:         s.Agents,
		Workflows:      s.Workflows,
		Tx:             s.Pool,
		Launcher:       NewLauncher(tmux, "127.0.0.1:9999"),
		Harnesses:      fakeHarnesses{harness: &fakeHarness{}},
		Lifecycle:      lifecycle,
		DefaultHarness: testHarnessID,
		MaxAgents:      maxAgents,
	})
}

// fakeLifecycle stands in for running a workflow's actions, so a create's tests drive the
// whole path without an actions directory or a process. It records what it was asked for,
// which is what says the workflow's own choice reached it.
type fakeLifecycle struct {
	// dir is what setup names as the directory to run in, empty for setup that names none.
	dir      string
	setupErr error
	setup    []string
	cleanup  []string
}

func (f *fakeLifecycle) RunSetupAction(_ context.Context, _ agentstore.Agent, name string) (string, error) {
	f.setup = append(f.setup, name)
	if f.setupErr != nil {
		return "", f.setupErr
	}

	return f.dir, nil
}

func (f *fakeLifecycle) RunCleanupAction(_ context.Context, _ agentstore.Agent, name string) error {
	f.cleanup = append(f.cleanup, name)

	return nil
}

// workflowService builds the workflow service over these stores.
func (s *testStore) workflowService() *WorkflowService {
	return NewWorkflowService(WorkflowParams{Workflows: s.Workflows, Schedules: s.Schedules, Tx: s.Pool})
}

// seed inserts an agent in a given state and returns it with its generated id.
func seed(t *testing.T, st *testStore, name, pane, state string) agentstore.Agent {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, st.Create(ctx, agentstore.Agent{Name: name, Pane: pane, State: state}))

	got, err := st.Get(ctx, name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return *got
}

// paneStamper is the one thing seedRunning needs, which both the fake and the real
// tmux client provide, so a test against either arranges a running agent the same
// way.
type paneStamper interface {
	SetPaneOwner(pane, agentID string) error
}

// seedRunning inserts an agent and stamps its pane with the id the store gave it,
// which is what a launched agent looks like: a row, plus a pane that answers to it.
// Seeding without the stamp describes an agent whose recorded pane is somebody
// else's, so a test wanting a live agent has to arrange both.
func seedRunning(t *testing.T, st *testStore, tmux paneStamper, name, pane, state string) agentstore.Agent {
	t.Helper()
	agent := seed(t, st, name, pane, state)
	require.NoError(t, tmux.SetPaneOwner(pane, agent.ID))

	return agent
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
	service, _ := newManagerWithHarness(tmux, st, flags)

	return service
}

// workflowsFor builds the workflow service over the same store, for the tests
// that set an agent's workflow up before acting on the agent.
func workflowsFor(st *testStore) *WorkflowService {
	return st.workflowService()
}

// newManagerWithHarness is the same, handing back the harness as well, for a test
// that asserts on what the panel asked the CLI to be started with. The launcher is
// pointed at a fixed address so what a spawned agent is told does not depend on the
// machine.
func newManagerWithHarness(tmux *fakeTmux, st *testStore, flags string) (*AgentService, *fakeHarness) {
	h := &fakeHarness{}

	return st.agentService(tmux, fakeHarnesses{harness: h}, "127.0.0.1:9999", flags), h
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
