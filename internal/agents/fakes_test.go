package agents

import (
	"context"
	"errors"
	"testing"

	"claude-agents/internal/pgtest"
	"claude-agents/internal/store"
	sessionstore "claude-agents/internal/store/session"
	transcriptstore "claude-agents/internal/store/transcript"
	workflowstore "claude-agents/internal/store/workflow"

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
// one pool. Nothing in production holds this: each service is still constructed
// with only the store it depends on. It exists so a test can arrange rows across
// domains without threading three handles through every helper.
type testStore struct {
	pool        *store.Pool
	chats       *sessionstore.Store
	workflows   *workflowstore.Store
	transcripts *transcriptstore.Store
}

func newStore(t *testing.T) *testStore {
	t.Helper()
	pool, err := store.Open(t.Context(), pgtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	return &testStore{
		pool:        pool,
		chats:       sessionstore.New(pool.DB()),
		workflows:   workflowstore.New(pool.DB()),
		transcripts: transcriptstore.New(pool.DB()),
	}
}

func (s *testStore) Close() error { return s.pool.Close() }

func (s *testStore) Get(ctx context.Context, name string) (*sessionstore.Agent, error) {
	return s.chats.Get(ctx, name)
}

func (s *testStore) Upsert(ctx context.Context, agent sessionstore.Agent) error {
	return s.chats.Upsert(ctx, agent)
}

func (s *testStore) List(ctx context.Context) ([]sessionstore.Agent, error) {
	return s.chats.List(ctx)
}

func (s *testStore) ListAll(ctx context.Context) ([]sessionstore.Agent, error) {
	return s.chats.ListAll(ctx)
}

func (s *testStore) SoftDelete(ctx context.Context, name string) error {
	return s.chats.Archive(ctx, name)
}

func (s *testStore) GetWorkflow(ctx context.Context, id string) (*workflowstore.Workflow, error) {
	return s.workflows.Get(ctx, id)
}

func (s *testStore) GetTranscript(ctx context.Context, sessionID string) (transcriptstore.Transcript, error) {
	return s.transcripts.Get(ctx, sessionID)
}

// sessionService builds the service under test over these stores, the same way
// the composition root does.
func (s *testStore) sessionService(tmux sessionTmux, launcher *Launcher, flags string) *SessionService {
	return NewSessionService(SessionParams{
		Tmux:        tmux,
		Sessions:    s.chats,
		Workflows:   s.workflows,
		Tx:          s.pool,
		Launcher:    launcher,
		ClaudeFlags: flags,
	})
}

// workflowService builds the workflow service over these stores.
func (s *testStore) workflowService() *WorkflowService {
	return NewWorkflowService(WorkflowParams{Workflows: s.workflows, Tx: s.pool})
}

// seed inserts an agent in a given state and returns it with its generated id.
func seed(t *testing.T, st *testStore, name, pane, state string) sessionstore.Agent {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, st.Upsert(ctx, sessionstore.Agent{Name: name, Pane: pane, State: state}))

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
