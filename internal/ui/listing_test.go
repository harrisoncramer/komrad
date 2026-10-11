package ui_test

import (
	"errors"
	"testing"

	"komrad/internal/engine"
	"komrad/internal/ui"
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
		open         func(t *testing.T, frames *harnessFrameFake) *panelHarness
		setup        func(harness *panelHarness)
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
			setup:   func(harness *panelHarness) { answerAgentCreate(harness, "") },
			press:   []string{"k", "space", "c", "h", "i", "enter"},
			settle:  []*watchv1.WatchEvent{buildPuts(newAgent("a2", "new", "w1"))},
			wantRow: "├─ new",
		},
		{
			name:    "a created workflow settles once its header arrives",
			setup:   answerWorkflowCreate,
			press:   []string{"k", "space", "w", "w", "e", "b", "enter"},
			settle:  []*watchv1.WatchEvent{buildPuts(newLiveWorkflow("w2", "web"))},
			wantRow: "web",
		},
		{
			name: "a row the watch delivered before the write answered settles it at once",
			setup: func(harness *panelHarness) {
				answerAgentCreate(harness, "")
				harness.watch(buildPuts(newAgent("a2", "new", "w1")))
			},
			press:   []string{"k", "k", "space", "c", "h", "i", "enter"},
			wantRow: "├─ new",
		},
		{
			name: "a refused create parks nothing",
			setup: func(harness *panelHarness) {
				harness.refuse(agentv1connect.AgentServiceCreateAgentProcedure, connect.NewError(connect.CodeInvalidArgument, errors.New("not allowed")))
			},
			press:   []string{"k", "space", "c", "h", "i", "enter", "esc"},
			settle:  []*watchv1.WatchEvent{buildPuts(newAgent("a2", "new", "w1"))},
			wantRow: "api",
		},
		{
			name:    "a row arriving after the parked write ran out of time leaves the cursor where it was",
			setup:   func(harness *panelHarness) { answerAgentCreate(harness, "") },
			press:   []string{"k", "space", "c", "h", "i", "enter"},
			expire:  true,
			settle:  []*watchv1.WatchEvent{buildPuts(newAgent("a2", "new", "w1"))},
			wantRow: "api",
		},
		{
			name: "a key pressed while a write is parked takes the cursor back",
			open: func(t *testing.T, _ *harnessFrameFake) *panelHarness {
				return openAgents(t, newLiveWorkflow("w1", "api"), newDormant("old"))
			},
			setup:       answerResume,
			press:       []string{"j", "enter"},
			parkedPress: []string{"k"},
			settle:      []*watchv1.WatchEvent{buildPuts(newRunning())},
			wantRow:     "api",
		},
		{
			name:         "a key pressed while a focused write is parked still shows and selects its pane",
			open:         openSettlingTelescope,
			setup:        answerOrchestratorStart,
			press:        []string{"k", "enter"},
			parkedPress:  []string{"j"},
			wait:         true,
			settle:       []*watchv1.WatchEvent{buildPuts(newOrchestrator())},
			showsLate:    true,
			wantRow:      "└─ telescope",
			wantGuest:    "%9",
			wantSelected: []string{"%9"},
		},
		{
			name: "a focused write settled by one watch batch selects its pane once it is swapped in",
			open: openSettlingBackend,
			setup: func(harness *panelHarness) {
				answerWorkflowCreate(harness)
				answerOrchestratorStart(harness)
			},
			press:        []string{"space", "w", "a", "p", "i", "enter"},
			wait:         true,
			settle:       []*watchv1.WatchEvent{buildPuts(newLiveWorkflow("w2", "api"), newWorkflowOrchestrator("w2"))},
			wantGuest:    "%9",
			wantSelected: []string{"%9"},
		},
		{
			name:    "a row the filter hides leaves the cursor on a row it can see",
			setup:   func(harness *panelHarness) { answerAgentCreate(harness, "") },
			press:   []string{"/", "tab", "^", "o", "l", "d", "$", "enter", "k", "space", "c", "h", "i", "enter"},
			settle:  []*watchv1.WatchEvent{buildPuts(newAgent("a2", "new", "w1"))},
			wantRow: "api",
		},
		{
			name:         "a batch for another row after the write settled keeps the pane the write showed",
			open:         openSettlingStaged,
			setup:        func(harness *panelHarness) { answerAgentCreate(harness, "%9") },
			press:        []string{"k", "space", "c", "h", "i", "enter"},
			settle:       []*watchv1.WatchEvent{buildPuts(newCreatedAgent()), buildPuts(newStagedOld("renamed"))},
			wantRow:      "├─ check-ci",
			wantGuest:    "%9",
			wantSelected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			frames := &harnessFrameFake{isShowing: !tt.showsLate}
			open := tt.open
			if open == nil {
				open = func(t *testing.T, _ *harnessFrameFake) *panelHarness { return openSettling(t) }
			}
			harness := open(t, frames)
			tt.setup(harness)

			harness.press(tt.press...)
			harness.press(tt.parkedPress...)
			if tt.wait {
				harness.fireTimers()
			}
			if tt.expire {
				harness.panel.ExpireParked()
				harness.fireMatching(ui.IsWarmupTick)
			}
			for _, event := range tt.settle {
				harness.watch(event)
			}
			if tt.showsLate {
				frames.isShowing = true
				harness.fireTimers()
			}

			if tt.wantRow != "" {
				assert.Equal(t, tt.wantRow, readHoveredRow(harness.readFrame()))
			}
			if tt.wantGuest != "" {
				assert.Equal(t, tt.wantGuest, harness.panel.ReadGuestPane())
				assert.ElementsMatch(t, tt.wantSelected, harness.panel.ReadSelectedPanes())
				assert.Empty(t, harness.panel.ReadSelectedUnswapped())
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

		harness := openTerminal(t, panelHarnessParams{}, newStagedShellAgent(t.TempDir()))
		harness.refuse(agentv1connect.AgentServiceUpdateAgentProcedure, connect.NewError(connect.CodeAlreadyExists, errors.New("name is taken")))
		harness.press("space", "t")
		opened := len(harness.panel.ReadRespawns())
		require.Positive(t, opened, "the shell is opened into a pane")

		harness.press("r", "x", "enter", "esc")

		assert.NotContains(t, harness.panel.ReadKilledPanes(), infoPane, "the shell's pane is left where it is")
	})

	t.Run("the archive cursor keeps its agent when a folder above it goes", func(t *testing.T) {
		t.Parallel()

		harness := openArchivedAgents(t,
			newLiveWorkflow("w1", "api"),
			newLiveWorkflow("w2", "infra"),
			newArchivedAgent("x1", "api-spike", "w1"),
			newArchivedAgent("x2", "infra-audit", "w2"),
			newArchivedAgent("x3", "infra-upgrade", "w2"),
		)
		harness.press("j", "j", "j")
		require.Equal(t, "├─ infra-upgrade", readHoveredRow(harness.readFrame()))

		harness.watch(buildSnapshot(
			newLiveWorkflow("w2", "infra"),
			newArchivedAgent("x2", "infra-audit", "w2"),
			newArchivedAgent("x3", "infra-upgrade", "w2"),
		))

		assert.Equal(t, "├─ infra-upgrade", readHoveredRow(harness.readFrame()))
	})
}

// TestListingFocus covers which writes and keys hand the keyboard to the pane they show.
func TestListingFocus(t *testing.T) {
	t.Parallel()

	t.Run("the open key over a staged agent hands it the keys", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, newLiveWorkflow("w1", "api"), newRunning())
		harness.press("j")
		harness.fireTimers()

		harness.press("enter")

		assert.Equal(t, []string{"%9"}, harness.panel.ReadSelectedPanes())
	})

	t.Run("the open key over an agent the stage could not show leaves the keys", func(t *testing.T) {
		t.Parallel()

		harness := newPanelHarness(t, panelHarnessParams{refuseSplits: true})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "api"), newRunning()))

		harness.press("j")
		harness.fireTimers()
		harness.press("enter")

		assert.Empty(t, harness.panel.ReadSelectedPanes())
	})

	t.Run("a create stages its pane and leaves the keys on the panel", func(t *testing.T) {
		t.Parallel()

		harness := openSettling(t)
		answerAgentCreate(harness, "%9")

		harness.press("k", "space", "c", "h", "i", "enter")
		harness.watch(buildPuts(newCreatedAgent()))

		assert.Equal(t, "%9", harness.panel.ReadGuestPane())
		assert.Empty(t, harness.panel.ReadSelectedPanes())
	})

	t.Run("a resume leaves the keys on the panel", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, newLiveWorkflow("w1", "api"), newDormant("old"))
		answerResume(harness)

		harness.press("j", "enter")

		assert.Empty(t, harness.panel.ReadGuestPane())
		assert.Empty(t, harness.panel.ReadSelectedPanes())
	})
}

// TestListingUnreachable covers the server going away being said once per time it goes.
func TestListingUnreachable(t *testing.T) {
	t.Run("the tree already rendered stays rendered", func(t *testing.T) {
		harness := openUnreachable(t, engine.Remote{})

		frame := harness.readFrame()
		assert.Contains(t, frame, "Cannot reach the")
		assert.Equal(t, []string{"backend"}, readListRows(frame))
	})

	t.Run("a stream lost twice in a row is said once", func(t *testing.T) {
		harness := openUnreachable(t, engine.Remote{})

		harness.send(ui.NewWatchLost(errors.New("connection refused")))

		frame := harness.readFrame()
		assert.Contains(t, frame, "Cannot reach the")
		assert.NotContains(t, frame, "The panel could not list your agents", "and no screen is taken over")
		assert.Contains(t, readOpenMessages(harness), "messages 1 of 1", "a server still down says nothing more")
	})

	t.Run("a close the service chose leaves a lost server lost", func(t *testing.T) {
		harness := openUnreachable(t, engine.Remote{})

		harness.send(ui.NewWatchLost(nil))
		harness.send(ui.NewWatchLost(errors.New("connection refused")))

		assert.Contains(t, readOpenMessages(harness), "messages 1 of 1")
	})

	t.Run("a stream that came back and went again is said again", func(t *testing.T) {
		harness := openUnreachable(t, engine.Remote{})

		harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend")))
		harness.send(ui.NewWatchLost(errors.New("connection refused")))

		assert.Contains(t, readOpenMessages(harness), "messages 1 of 2")
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
			name:   "the hint for a remote engine says the tunnel is dialled again",
			remote: engine.Remote{Destination: "komrad@host"},
			want:   "The tunnel to the engine is dialled again until it answers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openUnreachable(t, tt.remote)

			frame := readOpenMessages(harness)
			assert.Contains(t, frame, serverAddr)
			assert.Contains(t, frame, tt.want)
		})
	}
}

const serverAddr = "127.0.0.1:8787"

type harnessFrameFake struct {
	isShowing bool
}

func (f *harnessFrameFake) DoesFrameShowHarness(string, string) bool {
	return f.isShowing
}

func openSettling(t *testing.T) *panelHarness {
	t.Helper()

	harness := openAgents(t, newLiveWorkflow("w1", "api"), newAgent("a1", "old", "w1"))
	harness.press("j")

	return harness
}

func openSettlingTelescope(t *testing.T, frames *harnessFrameFake) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, panelHarnessParams{harnessFrames: frames})
	answerWorktreeReads(harness)
	harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend"), newAgent("a1", "telescope", "w1")))

	return harness
}

func openSettlingBackend(t *testing.T, frames *harnessFrameFake) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, panelHarnessParams{harnessFrames: frames})
	answerWorktreeReads(harness)
	harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend")))

	return harness
}

func openSettlingStaged(t *testing.T, _ *harnessFrameFake) *panelHarness {
	t.Helper()

	harness := openAgents(t, newLiveWorkflow("w1", "api"), newStagedOld("old"))
	harness.fireTimers()
	require.Equal(t, "%1", harness.panel.ReadGuestPane())

	return harness
}

func newStagedOld(name string) *agentv1.Agent {
	old := newAgent("a1", name, "w1")
	old.PaneId = "%1"
	old.Alive = xptr.Ptr(true)

	return old
}

func newWorkflowOrchestrator(workflow string) *agentv1.Agent {
	orch := newOrchestrator()
	orch.WorkflowId = workflow

	return orch
}

func answerAgentCreate(harness *panelHarness, pane string) {
	harness.answer(agentv1connect.AgentServiceCreateAgentProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&agentv1.CreateAgentResponse{Agent: &agentv1.Agent{Id: "a2", PaneId: pane}}), nil
	})
}

func newCreatedAgent() *agentv1.Agent {
	agent := newAgent("a2", "check-ci", "w1")
	agent.PaneId = "%9"
	agent.Alive = xptr.Ptr(true)

	return agent
}

func answerResume(harness *panelHarness) {
	harness.answer(agentv1connect.AgentServiceResumeAgentProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&agentv1.ResumeAgentResponse{Agent: &agentv1.Agent{Id: "a1", PaneId: "%9"}}), nil
	})
}

func newRunning() *agentv1.Agent {
	agent := newAgent("a1", "old", "w1")
	agent.PaneId = "%9"
	agent.Alive = xptr.Ptr(true)

	return agent
}

func openUnreachable(t *testing.T, remote engine.Remote) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, panelHarnessParams{
		settings: ui.PanelSettings{Addr: serverAddr},
		engine:   remote,
	})
	harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend")))
	harness.send(ui.NewWatchLost(errors.New("connection refused")))

	return harness
}

func newDormant(name string) *agentv1.Agent {
	return &agentv1.Agent{
		Id:         "a1",
		Name:       name,
		WorkflowId: "w1",
		State:      agentv1.AgentState_AGENT_STATE_IDLE,
		Alive:      xptr.Ptr(false),
	}
}

func answerOrchestratorStart(harness *panelHarness) {
	harness.answer(agentv1connect.AgentServiceStartOrchestratorProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&agentv1.StartOrchestratorResponse{Agent: &agentv1.Agent{
			Id:      "o1",
			PaneId:  "%9",
			Harness: "claude",
		}}), nil
	})
}
