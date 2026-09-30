package ui_test

import (
	"strings"
	"testing"

	"komrad/internal/panes"
	"komrad/internal/ui"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

type bindRecorder struct {
	bound   map[string][]string
	unbound []string
	window  string
}

// TestSession covers the panel's own tmux session.
func TestSession(t *testing.T) {
	t.Run("no sidebar binds nothing", func(t *testing.T) {
		tmux := &bindRecorder{window: "@1"}

		ui.NewSession(ui.NewSessionParams{
			Tmux:        tmux,
			Session:     "komrad",
			LeaderKey:   "C-Space",
			LeaderTable: "komrad-leader",
		}).HoldLeader("")

		assert.Empty(t, tmux.bound)
	})

	t.Run("the panel going gives the leader back", func(t *testing.T) {
		tmux := &bindRecorder{window: "@1"}
		session := ui.NewSession(ui.NewSessionParams{
			Tmux:        tmux,
			Session:     "komrad",
			LeaderKey:   "C-Space",
			LeaderTable: "komrad-leader",
		})

		session.HoldLeader("%3")
		require.NotEmpty(t, tmux.bound)
		session.ReleaseLeader()

		assert.Empty(t, tmux.bound)
		assert.Equal(t, []string{"C-Space"}, tmux.unbound, "tmux keeps a root binding until the server dies")
	})

	t.Run("releasing a leader nobody held says nothing to tmux", func(t *testing.T) {
		tmux := &bindRecorder{}

		ui.NewSession(ui.NewSessionParams{
			Tmux:        tmux,
			Session:     "komrad",
			LeaderKey:   "C-Space",
			LeaderTable: "komrad-leader",
		}).ReleaseLeader()

		assert.Empty(t, tmux.unbound)
	})
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

func (b *bindRecorder) BindTableKey(table, key string, command ...string) {
	if b.bound == nil {
		b.bound = map[string][]string{}
	}

	b.bound[table+":"+key] = command
}

func (b *bindRecorder) UnbindTable(table string) {
	for bound := range b.bound {
		if strings.HasPrefix(bound, table+":") {
			delete(b.bound, bound)
		}
	}
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

func (b *bindRecorder) GetWindowID(string) string {
	return b.window
}

func (b *bindRecorder) TuneCopyMode(string) {}

func (b *bindRecorder) OpenCopyStandIn(panes.CopyStandIn) error {
	return nil
}
