package ui_test

import (
	"errors"
	"testing"
	"time"

	"komrad/internal/engine"
	"komrad/internal/ui"
	"komrad/internal/ui/paneltest"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"

	watchv1 "komrad/proto/gen/watch/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListingSettlesWrites covers a write held until the watch shows what it did, and where it
// leaves the cursor and the stage once it settles.
func TestListingSettlesWrites(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		open         func(t *testing.T, frames *paneltest.HarnessFrames) *paneltest.Panel
		setup        func(harness *paneltest.Panel)
		press        []string
		parkedPress  []string
		wait         bool
		expire       bool
		settle       []*watchv1.WatchEvent
		showsLate    bool
		wantRow      string
		wantGuest    string
		wantSelected []string
	}{
		{
			name:    "a created agent is parked until the watch carries it",
			setup:   func(harness *paneltest.Panel) { harness.AnswerAgentCreate("") },
			press:   []string{"k", "space", "c", "h", "i", "enter"},
			settle:  []*watchv1.WatchEvent{paneltest.Puts(paneltest.Agent("a2", "new", "w1"))},
			wantRow: "├─ new",
		},
		{
			name:    "a created workflow settles once its header arrives",
			setup:   (*paneltest.Panel).AnswerWorkflowCreate,
			press:   []string{"k", "space", "w", "w", "e", "b", "enter"},
			settle:  []*watchv1.WatchEvent{paneltest.Puts(paneltest.Workflow("w2", "web"))},
			wantRow: "web",
		},
		{
			name: "a row the watch delivered before the write answered settles it at once",
			setup: func(harness *paneltest.Panel) {
				harness.AnswerAgentCreate("")
				harness.Watch(paneltest.Puts(paneltest.Agent("a2", "new", "w1")))
			},
			press:   []string{"k", "k", "space", "c", "h", "i", "enter"},
			wantRow: "├─ new",
		},
		{
			name: "a refused create parks nothing",
			setup: func(harness *paneltest.Panel) {
				harness.Refuse(agentv1connect.AgentServiceCreateAgentProcedure, connect.NewError(connect.CodeInvalidArgument, errors.New("not allowed")))
			},
			press:   []string{"k", "space", "c", "h", "i", "enter", "esc"},
			settle:  []*watchv1.WatchEvent{paneltest.Puts(paneltest.Agent("a2", "new", "w1"))},
			wantRow: "api",
		},
		{
			name:    "a row arriving after the parked write ran out of time leaves the cursor where it was",
			setup:   func(harness *paneltest.Panel) { harness.AnswerAgentCreate("") },
			press:   []string{"k", "space", "c", "h", "i", "enter"},
			expire:  true,
			settle:  []*watchv1.WatchEvent{paneltest.Puts(paneltest.Agent("a2", "new", "w1"))},
			wantRow: "api",
		},
		{
			name: "a key pressed while a write is parked takes the cursor back",
			open: func(t *testing.T, _ *paneltest.HarnessFrames) *paneltest.Panel {
				return paneltest.OpenAgents(t, paneltest.Workflow("w1", "api"), paneltest.Dormant("old"))
			},
			setup:       (*paneltest.Panel).AnswerResume,
			press:       []string{"j", "enter"},
			parkedPress: []string{"k"},
			settle:      []*watchv1.WatchEvent{paneltest.Puts(paneltest.Running())},
			wantRow:     "api",
		},
		{
			name:         "a key pressed while a focused write is parked still shows and selects its pane",
			open:         openSettlingTelescope,
			setup:        answerOrchestratorStart,
			press:        []string{"k", "enter"},
			parkedPress:  []string{"j"},
			wait:         true,
			settle:       []*watchv1.WatchEvent{paneltest.Puts(paneltest.Orchestrator())},
			showsLate:    true,
			wantRow:      "└─ telescope",
			wantGuest:    "%9",
			wantSelected: []string{"%9"},
		},
		{
			name: "a focused write settled by one watch batch selects its pane once it is swapped in",
			open: openSettlingBackend,
			setup: func(harness *paneltest.Panel) {
				harness.AnswerWorkflowCreate()
				answerOrchestratorStart(harness)
			},
			press:        []string{"space", "w", "a", "p", "i", "enter"},
			wait:         true,
			settle:       []*watchv1.WatchEvent{paneltest.Puts(paneltest.Workflow("w2", "api"), newWorkflowOrchestrator("w2"))},
			wantGuest:    "%9",
			wantSelected: []string{"%9"},
		},
		{
			name:    "a row the filter hides leaves the cursor on a row it can see",
			setup:   func(harness *paneltest.Panel) { harness.AnswerAgentCreate("") },
			press:   []string{"/", "tab", "^", "o", "l", "d", "$", "enter", "k", "space", "c", "h", "i", "enter"},
			settle:  []*watchv1.WatchEvent{paneltest.Puts(paneltest.Agent("a2", "new", "w1"))},
			wantRow: "api",
		},
		{
			name:         "a batch for another row after the write settled keeps the pane the write showed",
			open:         openSettlingStaged,
			setup:        func(harness *paneltest.Panel) { harness.AnswerAgentCreate("%9") },
			press:        []string{"k", "space", "c", "h", "i", "enter"},
			settle:       []*watchv1.WatchEvent{paneltest.Puts(paneltest.CreatedAgent()), paneltest.Puts(newStagedOld("renamed"))},
			wantRow:      "├─ check-ci",
			wantGuest:    "%9",
			wantSelected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			frames := &paneltest.HarnessFrames{IsShowing: !tt.showsLate}
			open := tt.open
			if open == nil {
				open = func(t *testing.T, _ *paneltest.HarnessFrames) *paneltest.Panel { return openSettling(t) }
			}
			harness := open(t, frames)
			tt.setup(harness)

			harness.Press(tt.press...)
			harness.Press(tt.parkedPress...)
			if tt.wait {
				harness.Advance(time.Second)
			}
			if tt.expire {
				harness.Advance(5 * time.Second)
			}
			for _, event := range tt.settle {
				harness.Watch(event)
			}
			if tt.showsLate {
				frames.IsShowing = true
				harness.Advance(time.Second)
			}

			if tt.wantRow != "" {
				assert.Equal(t, tt.wantRow, paneltest.HoveredRow(harness.Frame()))
			}
			if tt.wantGuest != "" {
				assert.Equal(t, tt.wantGuest, harness.Tmux().GuestPane())
				assert.ElementsMatch(t, tt.wantSelected, harness.Tmux().SelectedPanes())
				assert.Empty(t, harness.Tmux().SelectedUnswapped())
			}
		})
	}
}

// TestListingKeepsItsPlace covers what the panel shows staying put through a write or a watch
// batch that has nothing to do with it.
func TestListingKeepsItsPlace(t *testing.T) {
	t.Parallel()

	t.Run("a write with nothing to do with an open info pane leaves it up", func(t *testing.T) {
		t.Parallel()

		harness := openTerminal(t, paneltest.Params{}, newStagedShellAgent(t.TempDir()))
		harness.Refuse(agentv1connect.AgentServiceUpdateAgentProcedure, connect.NewError(connect.CodeAlreadyExists, errors.New("name is taken")))
		harness.Press("space", "t")
		opened := len(harness.Tmux().Respawns())
		require.Positive(t, opened, "the shell is opened into a pane")

		harness.Press("r", "x", "enter", "esc")

		assert.NotContains(t, harness.Tmux().KilledPanes(), infoPane, "the shell's pane is left where it is")
	})

	t.Run("the archive cursor keeps its agent when a folder above it goes", func(t *testing.T) {
		t.Parallel()

		harness := paneltest.OpenArchivedAgents(t,
			paneltest.Workflow("w1", "api"),
			paneltest.Workflow("w2", "infra"),
			paneltest.ArchivedAgent("x1", "api-spike", "w1"),
			paneltest.ArchivedAgent("x2", "infra-audit", "w2"),
			paneltest.ArchivedAgent("x3", "infra-upgrade", "w2"),
		)
		harness.Press("j", "j", "j")
		require.Equal(t, "├─ infra-upgrade", paneltest.HoveredRow(harness.Frame()))

		harness.Watch(paneltest.Snapshot(
			paneltest.Workflow("w2", "infra"),
			paneltest.ArchivedAgent("x2", "infra-audit", "w2"),
			paneltest.ArchivedAgent("x3", "infra-upgrade", "w2"),
		))

		assert.Equal(t, "├─ infra-upgrade", paneltest.HoveredRow(harness.Frame()))
	})
}

// TestListingFocus covers which writes and keys hand the keyboard to the pane they show.
func TestListingFocus(t *testing.T) {
	t.Parallel()

	t.Run("the open key over a staged agent hands it the keys", func(t *testing.T) {
		t.Parallel()

		harness := paneltest.OpenAgents(t, paneltest.Workflow("w1", "api"), paneltest.Running())
		harness.Press("j")
		harness.Advance(time.Second)

		harness.Press("enter")

		assert.Equal(t, []string{"%9"}, harness.Tmux().SelectedPanes())
	})

	t.Run("the open key over an agent the stage could not show leaves the keys", func(t *testing.T) {
		t.Parallel()

		harness := paneltest.Start(t, paneltest.Params{RefuseSplits: true})
		harness.AnswerWorktreeReads()
		harness.Watch(paneltest.Snapshot(paneltest.Workflow("w1", "api"), paneltest.Running()))

		harness.Press("j")
		harness.Advance(time.Second)
		harness.Press("enter")

		assert.Empty(t, harness.Tmux().SelectedPanes())
	})

	t.Run("a create stages its pane and leaves the keys on the panel", func(t *testing.T) {
		t.Parallel()

		harness := openSettling(t)
		harness.AnswerAgentCreate("%9")

		harness.Press("k", "space", "c", "h", "i", "enter")
		harness.Watch(paneltest.Puts(paneltest.CreatedAgent()))

		assert.Equal(t, "%9", harness.Tmux().GuestPane())
		assert.Empty(t, harness.Tmux().SelectedPanes())
	})

	t.Run("a resume leaves the keys on the panel", func(t *testing.T) {
		t.Parallel()

		harness := paneltest.OpenAgents(t, paneltest.Workflow("w1", "api"), paneltest.Dormant("old"))
		harness.AnswerResume()

		harness.Press("j", "enter")

		assert.Empty(t, harness.Tmux().GuestPane())
		assert.Empty(t, harness.Tmux().SelectedPanes())
	})
}

// TestListingUnreachable covers the server going away being said once per time it goes.
func TestListingUnreachable(t *testing.T) {
	t.Run("the tree already rendered stays rendered", func(t *testing.T) {
		harness := openUnreachable(t, engine.Remote{})

		frame := harness.Frame()
		assert.Contains(t, frame, "Cannot reach the")
		assert.Equal(t, []string{"backend"}, paneltest.ListRows(frame))
	})

	t.Run("a stream lost twice in a row is said once", func(t *testing.T) {
		harness := openUnreachable(t, engine.Remote{})

		harness.LoseWatch(errors.New("connection refused"))

		frame := harness.Frame()
		assert.Contains(t, frame, "Cannot reach the")
		assert.NotContains(t, frame, "The panel could not list your agents", "and no screen is taken over")
		assert.Contains(t, harness.ReadMessages(), "messages 1 of 1", "a server still down says nothing more")
	})

	t.Run("a close the server chose leaves a lost server lost", func(t *testing.T) {
		harness := openUnreachable(t, engine.Remote{})

		harness.LoseWatch(nil)
		harness.LoseWatch(errors.New("connection refused"))

		assert.Contains(t, harness.ReadMessages(), "messages 1 of 1")
	})

	t.Run("a stream that came back and went again is said again", func(t *testing.T) {
		harness := openUnreachable(t, engine.Remote{})

		harness.Watch(paneltest.Snapshot(paneltest.Workflow("w1", "backend")))
		harness.LoseWatch(errors.New("connection refused"))

		assert.Contains(t, harness.ReadMessages(), "messages 1 of 2")
	})

	tests := []struct {
		name   string
		remote engine.Remote
		want   string
	}{
		{
			name: "the hint on this machine says how to start a server",
			want: "Start it with: komrad serve",
		},
		{
			name:   "the hint for a remote engine says the panel keeps trying it",
			remote: engine.Remote{URL: "https://komrad.example.com"},
			want:   "The panel keeps trying the engine until it answers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openUnreachable(t, tt.remote)

			frame := harness.ReadMessages()
			assert.Contains(t, frame, serverAddr)
			assert.Contains(t, frame, tt.want)
		})
	}
}

const serverAddr = "127.0.0.1:8787"

func openSettling(t *testing.T) *paneltest.Panel {
	t.Helper()

	harness := paneltest.OpenAgents(t, paneltest.Workflow("w1", "api"), paneltest.Agent("a1", "old", "w1"))
	harness.Press("j")

	return harness
}

func openSettlingTelescope(t *testing.T, frames *paneltest.HarnessFrames) *paneltest.Panel {
	t.Helper()

	harness := paneltest.Start(t, paneltest.Params{HarnessFrames: frames})
	harness.AnswerWorktreeReads()
	harness.Watch(paneltest.Snapshot(paneltest.Workflow("w1", "backend"), paneltest.Agent("a1", "telescope", "w1")))

	return harness
}

func openSettlingBackend(t *testing.T, frames *paneltest.HarnessFrames) *paneltest.Panel {
	t.Helper()

	harness := paneltest.Start(t, paneltest.Params{HarnessFrames: frames})
	harness.AnswerWorktreeReads()
	harness.Watch(paneltest.Snapshot(paneltest.Workflow("w1", "backend")))

	return harness
}

func openSettlingStaged(t *testing.T, _ *paneltest.HarnessFrames) *paneltest.Panel {
	t.Helper()

	harness := paneltest.OpenAgents(t, paneltest.Workflow("w1", "api"), newStagedOld("old"))
	harness.Advance(time.Second)
	require.Equal(t, "%4", harness.Tmux().GuestPane())

	return harness
}

func newStagedOld(name string) *agentv1.Agent {
	old := paneltest.Agent("a1", name, "w1")
	old.PaneId = "%4"
	old.Alive = xptr.Ptr(true)

	return old
}

func newWorkflowOrchestrator(workflow string) *agentv1.Agent {
	orch := paneltest.Orchestrator()
	orch.WorkflowId = workflow

	return orch
}

func openUnreachable(t *testing.T, remote engine.Remote) *paneltest.Panel {
	t.Helper()

	harness := paneltest.Start(t, paneltest.Params{
		Settings: ui.PanelSettings{Addr: serverAddr},
		Engine:   remote,
	})
	harness.Watch(paneltest.Snapshot(paneltest.Workflow("w1", "backend")))
	harness.LoseWatch(errors.New("connection refused"))

	return harness
}

func answerOrchestratorStart(harness *paneltest.Panel) {
	harness.Answer(agentv1connect.AgentServiceStartOrchestratorProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&agentv1.StartOrchestratorResponse{Agent: &agentv1.Agent{
			Id:      "o1",
			PaneId:  "%9",
			Harness: "claude",
		}}), nil
	})
}
