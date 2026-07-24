package agents

import (
	"context"
	"path/filepath"
	"testing"

	"claude-agents/internal/store"

	"github.com/stretchr/testify/require"
)

// fakeTmux is a scripted, recording tmux client. Reads are served from the
// alive/captures/attached maps; writes are appended to the recording slices so a
// test can assert what the manager sent. It implements the full tmuxClient
// interface and also satisfies the reconciler's narrower tmuxProbe.
type fakeTmux struct {
	alive     map[string]bool
	captures  map[string]string
	attached  map[string]bool
	paneIDs   map[string]string
	clientDir string
	clientSes string
	current   string

	sent        map[string][][]string
	globals     map[string]string
	renamed     [][2]string
	killed      []string
	switched    []string
	resized     map[string][2]int
	newSessions map[string]string

	newWindows  map[string]string // window name -> dir
	swaps       [][2]string       // {src, dst}
	respawns    [][]string        // pane + argv per respawn
	killedPanes []string

	detached       []string
	sessionOptions map[string]string
}

func newFakeTmux() *fakeTmux {
	return &fakeTmux{
		alive:          map[string]bool{},
		captures:       map[string]string{},
		attached:       map[string]bool{},
		paneIDs:        map[string]string{},
		sent:           map[string][][]string{},
		globals:        map[string]string{},
		resized:        map[string][2]int{},
		newSessions:    map[string]string{},
		newWindows:     map[string]string{},
		sessionOptions: map[string]string{},
	}
}

// CurrentPane returns a fixed nav pane id for the panel's own pane.
func (f *fakeTmux) CurrentPane() (string, error) { return "%nav", nil }

// SplitRight records the anchor and returns a fixed stage pane id.
func (f *fakeTmux) SplitRight(anchorPane, dir string) string { return "%stage" }

// NewWindow records the window and returns a synthetic pane id, marking it alive
// so pane-keyed liveness sees it.
func (f *fakeTmux) NewWindow(session, name, dir string) string {
	f.newWindows[name] = dir
	pane := "%" + name
	f.alive[pane] = true
	return pane
}

func (f *fakeTmux) SwapPanes(src, dst string) { f.swaps = append(f.swaps, [2]string{src, dst}) }

func (f *fakeTmux) RespawnPane(pane, dir string, argv ...string) {
	f.respawns = append(f.respawns, append([]string{pane}, argv...))
}

func (f *fakeTmux) KillPane(pane string) { f.killedPanes = append(f.killedPanes, pane) }

func (f *fakeTmux) Socket() string                { return "test" }
func (f *fakeTmux) SetGlobal(name, value string)  { f.globals[name] = value }
func (f *fakeTmux) PaneAlive(pane string) bool    { return f.alive[pane] }
func (f *fakeTmux) HasSession(name string) bool   { return f.alive[name] }
func (f *fakeTmux) SessionAttached(s string) bool { return f.attached[s] }
func (f *fakeTmux) CapturePane(pane string) string {
	return f.captures[pane]
}

func (f *fakeTmux) ResizeWindow(target string, width, height int) {
	f.resized[target] = [2]int{width, height}
}

func (f *fakeTmux) RenameSession(oldName, newName string) {
	f.renamed = append(f.renamed, [2]string{oldName, newName})
}

func (f *fakeTmux) ClientSession(string) string { return f.clientSes }
func (f *fakeTmux) SwitchToLast(client string)  { f.switched = append(f.switched, client) }
func (f *fakeTmux) KillSession(name string)     { f.killed = append(f.killed, name) }
func (f *fakeTmux) ClientPath(string) string    { return f.clientDir }

func (f *fakeTmux) NewDetachedSession(name, dir string) {
	f.newSessions[name] = dir
	f.alive[name] = true
}

// PaneID returns a scripted pane id for the session, defaulting to a synthetic
// id so a freshly created session always resolves to something non-empty.
func (f *fakeTmux) PaneID(target string) (string, error) {
	if pane, ok := f.paneIDs[target]; ok {
		return pane, nil
	}

	return "%" + target, nil
}

func (f *fakeTmux) SendKeys(target string, keys ...string) {
	f.sent[target] = append(f.sent[target], keys)
}

func (f *fakeTmux) CurrentSession() (string, error) { return f.current, nil }
func (f *fakeTmux) SetWindowSizeLatest(string)      {}
func (f *fakeTmux) ShowGlobal(name string) string   { return f.globals[name] }
func (f *fakeTmux) DetachSession(name string)       { f.detached = append(f.detached, name) }
func (f *fakeTmux) SetSessionOption(target, name, value string) {
	f.sessionOptions[target+"/"+name] = value
}

// lastKeys returns the most recent SendKeys payload sent to target.
func (f *fakeTmux) lastKeys(target string) []string {
	sends := f.sent[target]
	if len(sends) == 0 {
		return nil
	}

	return sends[len(sends)-1]
}

func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "agents.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	return st
}

// seed inserts an agent in a given state and returns it with its generated id.
func seed(t *testing.T, st *store.Store, name, pane, state string) store.Agent {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, st.Upsert(ctx, store.Agent{Name: name, Pane: pane, State: state}))

	got, err := st.Get(ctx, name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return *got
}

func stateOf(t *testing.T, st *store.Store, name string) string {
	t.Helper()
	got, err := st.Get(context.Background(), name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return got.State
}
