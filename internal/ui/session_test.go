package ui_test

import (
	"testing"

	"komrad/internal/ui"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

var cycleKeys = []string{"C-a", "C-q"}

type bindRecorder struct {
	client  string
	bound   map[string][]string
	unbound []string
}

// TestSession covers the panel's own tmux session.
func TestSession(t *testing.T) {
	t.Run("a client attached to the panel's session is somebody looking", func(t *testing.T) {
		assert.True(t, ui.NewSession(&bindRecorder{client: "/dev/ttys004"}, "komrad", cycleKeys).IsShown())
		assert.False(t, ui.NewSession(&bindRecorder{}, "komrad", cycleKeys).IsShown(), "the panel was hidden, which detached it")
	})

	t.Run("the cycle keys hand a press in any pane to the sidebar", func(t *testing.T) {
		tmux := &bindRecorder{}

		ui.NewSession(tmux, "komrad", cycleKeys).HoldCycleKeys("%3", true)

		assert.Equal(t, []string{"send-keys", "-t", "%3", "C-a"}, tmux.bound["root:C-a"])
		assert.Equal(t, []string{"send-keys", "-t", "%3", "C-q"}, tmux.bound["root:C-q"])
	})

	t.Run("no sidebar binds nothing", func(t *testing.T) {
		tmux := &bindRecorder{}

		ui.NewSession(tmux, "komrad", cycleKeys).HoldCycleKeys("", true)

		assert.Empty(t, tmux.bound)
	})

	t.Run("an empty stage gives the keys back once and then says nothing", func(t *testing.T) {
		tmux := &bindRecorder{}
		session := ui.NewSession(tmux, "komrad", cycleKeys)

		session.HoldCycleKeys("%3", true)
		session.HoldCycleKeys("%3", false)
		session.HoldCycleKeys("%3", false)

		assert.Empty(t, tmux.bound)
		assert.Equal(t, cycleKeys, tmux.unbound)
	})
}

// TestSessionCycleKeys covers the panel holding the cycle keys only while an agent's pane is on show.
func TestSessionCycleKeys(t *testing.T) {
	t.Run("the panel going gives the keys back", func(t *testing.T) {
		tmux := &bindRecorder{}
		session := ui.NewSession(tmux, "komrad", cycleKeys)

		session.HoldCycleKeys("%3", true)
		session.ReleaseCycleKeys()

		assert.Equal(t, cycleKeys, tmux.unbound, "tmux keeps a root binding until the server dies")
	})

	t.Run("releasing keys nobody held says nothing to tmux", func(t *testing.T) {
		tmux := &bindRecorder{}

		ui.NewSession(tmux, "komrad", cycleKeys).ReleaseCycleKeys()

		assert.Empty(t, tmux.unbound)
	})

	t.Run("a staged agent holds the keys once and a screen staging none gives them back", func(t *testing.T) {
		harness := openFocus(t)
		harness.watch(buildSnapshot(buildFocusRows()...))

		for _, key := range cycleKeys {
			command, bound := harness.panel.ReadRootBinding(key)
			require.True(t, bound)
			assert.Equal(t, []string{"send-keys", "-t", "%0", key}, command)
		}
		assert.Equal(t, 2, harness.panel.CountRootBinds(), "keys that are already right are left alone")

		given := len(harness.panel.ReadRootUnbinds())

		harness.press("shift+tab")

		assertCycleKeysGivenBack(t, harness, given)
	})

	t.Run("another agent taking the stage leaves the keys where they are", func(t *testing.T) {
		harness := openFocus(t)
		second := newFocusAgent("a2", "%12")
		second.CreatedAt = "2026-09-20T11:00:00Z"
		harness.watch(buildSnapshot(append(buildFocusRows(), second)...))

		harness.press("j")
		harness.fireTimers()

		require.Equal(t, "%12", harness.panel.ReadGuestPane())
		_, bound := harness.panel.ReadRootBinding(cycleKeys[0])
		assert.True(t, bound)
		assert.Equal(t, 2, harness.panel.CountRootBinds(), "the keys reach the sidebar whichever agent is staged")
	})

	t.Run("an agent whose pane goes gives the key back", func(t *testing.T) {
		harness := openFocus(t)
		rows := buildFocusRows()
		rows[1].(*agentv1.Agent).Alive = xptr.Ptr(false)
		rows[1].(*agentv1.Agent).PaneId = ""
		given := len(harness.panel.ReadRootUnbinds())

		harness.watch(buildSnapshot(rows...))

		assertCycleKeysGivenBack(t, harness, given)
	})

	t.Run("a form over the list gives the key back and closing it takes it again", func(t *testing.T) {
		harness := openFocus(t)
		given := len(harness.panel.ReadRootUnbinds())

		harness.press("r")
		assertCycleKeysGivenBack(t, harness, given)

		harness.press("esc")
		_, bound := harness.panel.ReadRootBinding(cycleKeys[0])
		assert.True(t, bound)
	})

	t.Run("a header row with the pane parked behind it gives the key back", func(t *testing.T) {
		harness := openFocus(t)
		given := len(harness.panel.ReadRootUnbinds())

		harness.press("k")
		harness.fireTimers()
		require.True(t, harness.panel.IsZoomed(), "a header row takes the window")
		require.Equal(t, "%9", harness.panel.ReadGuestPane(), "with the agent's pane left parked behind it")

		assertCycleKeysGivenBack(t, harness, given)
	})
}

func (b *bindRecorder) GetSessionClient(string) string {
	return b.client
}

func (b *bindRecorder) SetSessionOption(string, string, string) {}

func (b *bindRecorder) DetachSession(string) {}

func (b *bindRecorder) BindRootKey(key string, command ...string) {
	if b.bound == nil {
		b.bound = map[string][]string{}
	}

	b.bound["root:"+key] = command
}

func (b *bindRecorder) UnbindRootKey(key string) {
	delete(b.bound, "root:"+key)
	b.unbound = append(b.unbound, key)
}

func openFocus(t *testing.T) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, panelHarnessParams{})
	answerWorktreeReads(harness)
	harness.watch(buildSnapshot(buildFocusRows()...))

	return harness
}

func buildFocusRows() []proto.Message {
	return []proto.Message{newLiveWorkflow("w1", "backend"), newFocusAgent("a1", "%9")}
}

func newFocusAgent(id, pane string) *agentv1.Agent {
	agent := newAgentAt(id, "w1", 30)
	agent.PaneId = pane
	agent.Alive = xptr.Ptr(true)

	return agent
}

func assertCycleKeysGivenBack(t *testing.T, harness *panelHarness, given int) {
	t.Helper()

	for _, key := range cycleKeys {
		_, bound := harness.panel.ReadRootBinding(key)
		assert.False(t, bound)
	}
	assert.Equal(t, cycleKeys, harness.panel.ReadRootUnbinds()[given:], "given back once")
}
