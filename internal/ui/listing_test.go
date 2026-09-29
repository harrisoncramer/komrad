package ui_test

import (
	"errors"
	"testing"

	"komrad/internal/engine"
	"komrad/internal/ui"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

const serverAddr = "127.0.0.1:8787"

type drawReporter struct {
	isDrawn bool
}

func (f *drawReporter) DoesFrameShowHarness(string, string) bool {
	return f.isDrawn
}

// TestListingLandsWrites covers the cursor going to the row a write made once the watch carries it.
func TestListingLandsWrites(t *testing.T) {
	t.Parallel()

	t.Run("a created agent is waited for and then landed on", func(t *testing.T) {
		t.Parallel()

		harness := openLanding(t)
		answerAgentCreate(harness, "")

		harness.press("c", "h", "i", "ctrl+s")
		require.Equal(t, "  old", readHoveredRow(harness.readFrame()), "the write puts nothing on the list and the cursor waits")

		harness.watch(buildPuts(newAgent("a2", "new", "w1")))

		assert.Equal(t, "  new", readHoveredRow(harness.readFrame()))
	})

	t.Run("a created workflow is landed on once its header arrives", func(t *testing.T) {
		t.Parallel()

		harness := openLanding(t)
		answerWorkflowCreate(harness, "w2")

		harness.press("w", "w", "e", "b", "ctrl+s")
		harness.watch(buildPuts(newLiveWorkflow("w2", "web")))

		assert.Equal(t, "web", readHoveredRow(harness.readFrame()))
	})

	t.Run("a row the watch delivered before the write answered is landed on at once", func(t *testing.T) {
		t.Parallel()

		harness := openLanding(t)
		answerAgentCreate(harness, "")
		harness.watch(buildPuts(newAgent("a2", "new", "w1")))
		require.Equal(t, "  old", readHoveredRow(harness.readFrame()))

		harness.press("c", "h", "i", "ctrl+s")

		assert.Equal(t, "  new", readHoveredRow(harness.readFrame()))
	})

	t.Run("a refused create lands nowhere", func(t *testing.T) {
		t.Parallel()

		harness := openLanding(t)
		harness.refuse(agentv1connect.AgentServiceCreateAgentProcedure, connect.NewError(connect.CodeInvalidArgument, errors.New("not allowed")))

		harness.press("c", "h", "i", "ctrl+s", "esc")
		harness.watch(buildPuts(newAgent("a2", "new", "w1")))

		assert.Equal(t, "  old", readHoveredRow(harness.readFrame()))
	})

	t.Run("a row arriving after the landing gave up leaves the cursor where it was", func(t *testing.T) {
		t.Parallel()

		harness := openLanding(t)
		answerAgentCreate(harness, "")

		harness.press("c", "h", "i", "ctrl+s")
		harness.panel.ExpireLanding()
		harness.watch(buildPuts(newAgent("a2", "new", "w1")))

		assert.Equal(t, "  old", readHoveredRow(harness.readFrame()))
	})

	t.Run("a key pressed while the landing waits takes the cursor back", func(t *testing.T) {
		t.Parallel()

		harness := openLanding(t)
		answerAgentCreate(harness, "")

		harness.press("c", "h", "i", "ctrl+s", "k")
		harness.watch(buildPuts(newAgent("a2", "new", "w1")))

		assert.Equal(t, "api", readHoveredRow(harness.readFrame()))
	})

	t.Run("a row the filter hides leaves the cursor on a row it can see", func(t *testing.T) {
		t.Parallel()

		harness := openLanding(t)
		answerAgentCreate(harness, "")

		harness.press("/", "tab", "^", "o", "l", "d", "$", "enter")
		harness.press("c", "h", "i", "ctrl+s")
		harness.watch(buildPuts(newAgent("a2", "new", "w1")))

		assert.Equal(t, "  old", readHoveredRow(harness.readFrame()))
	})

	t.Run("a batch for another row keeps the pane the write showed", func(t *testing.T) {
		t.Parallel()

		old := newAgent("a1", "old", "w1")
		old.PaneId = "%1"
		old.Alive = xptr.Ptr(true)
		harness := openAgents(t, newLiveWorkflow("w1", "api"), old)
		harness.fireTimers()
		require.Equal(t, "%1", harness.panel.ReadGuestPane())
		answerAgentCreate(harness, "%9")

		harness.press("c", "h", "i", "ctrl+s")
		require.Equal(t, "%9", harness.panel.ReadGuestPane())

		renamed := proto.CloneOf(old)
		renamed.Name = "renamed"
		harness.watch(buildPuts(renamed))
		assert.Equal(t, "%9", harness.panel.ReadGuestPane(), "the old row's pane is not swapped back over the new one")

		created := newAgent("a2", "new", "w1")
		created.PaneId = "%9"
		created.Alive = xptr.Ptr(true)
		harness.watch(buildPuts(created))

		assert.Equal(t, "  new", readHoveredRow(harness.readFrame()))
		assert.Equal(t, "%9", harness.panel.ReadGuestPane())
	})

	t.Run("a write with nothing to do with an open info pane leaves it up", func(t *testing.T) {
		t.Parallel()

		harness := openTerminal(t, panelHarnessParams{}, newStagedShellAgent(t.TempDir()))
		harness.refuse(agentv1connect.AgentServiceUpdateAgentProcedure, connect.NewError(connect.CodeAlreadyExists, errors.New("name is taken")))
		harness.press("space", "t")
		opened := len(harness.panel.ReadRespawns())
		require.Positive(t, opened, "the shell is opened into a pane")

		harness.press("r", "x", "ctrl+s", "esc")

		assert.Empty(t, harness.panel.ReadKilledPanes(), "the shell's pane is left where it is")
	})

	t.Run("the archive cursor keeps its agent when a folder above it goes", func(t *testing.T) {
		t.Parallel()

		harness := openArchive(t,
			newLiveWorkflow("w1", "api"),
			newLiveWorkflow("w2", "infra"),
			newArchivedAgent("x1", "api-spike", "w1"),
			newArchivedAgent("x2", "infra-audit", "w2"),
			newArchivedAgent("x3", "infra-upgrade", "w2"),
		)
		harness.press("j", "j", "j")
		require.Equal(t, "  infra-upgrade", readHoveredRow(harness.readFrame()))

		harness.watch(buildSnapshot(
			newLiveWorkflow("w2", "infra"),
			newArchivedAgent("x2", "infra-audit", "w2"),
			newArchivedAgent("x3", "infra-upgrade", "w2"),
		))

		assert.Equal(t, "  infra-upgrade", readHoveredRow(harness.readFrame()))
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

	t.Run("a resume hands the keys to the pane it puts back", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, newLiveWorkflow("w1", "api"), newDormant("old"))
		answerResume(harness)

		harness.press("j", "enter")

		assert.Equal(t, "%9", harness.panel.ReadGuestPane())
		assert.Equal(t, []string{"%9"}, harness.panel.ReadSelectedPanes())
	})

	t.Run("a create stages its pane and leaves the keys on the panel", func(t *testing.T) {
		t.Parallel()

		harness := openLanding(t)
		answerAgentCreate(harness, "%9")

		harness.press("c", "h", "i", "ctrl+s")

		assert.Equal(t, "%9", harness.panel.ReadGuestPane())
		assert.Empty(t, harness.panel.ReadSelectedPanes())
	})

	t.Run("a resume waits for the harness to draw before handing over the keys", func(t *testing.T) {
		t.Parallel()

		frames := &drawReporter{}
		harness := newPanelHarness(t, panelHarnessParams{harnessFrames: frames})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "api"), newDormant("old")))
		answerResume(harness)

		harness.press("j", "enter")
		require.Empty(t, harness.panel.ReadSelectedPanes(), "a pane still at its shell is not given the keys")

		frames.isDrawn = true
		harness.fireTimers()

		assert.Equal(t, "%9", harness.panel.ReadGuestPane())
		assert.Equal(t, []string{"%9"}, harness.panel.ReadSelectedPanes())
	})

	t.Run("a resume tmux will not split for leaves the keys on the panel", func(t *testing.T) {
		t.Parallel()

		harness := newPanelHarness(t, panelHarnessParams{refuseSplits: true})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "api"), newDormant("old")))
		answerResume(harness)

		harness.press("j", "enter")

		assert.Empty(t, harness.panel.ReadGuestPane())
		assert.Empty(t, harness.panel.ReadSelectedPanes(), "the keys stay on the panel rather than moving to a pane nobody can see")
	})

	t.Run("a resume tmux will not split for once the harness draws leaves the keys too", func(t *testing.T) {
		t.Parallel()

		frames := &drawReporter{}
		harness := newPanelHarness(t, panelHarnessParams{harnessFrames: frames, refuseSplits: true})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "api"), newDormant("old")))
		answerResume(harness)

		harness.press("j", "enter")
		frames.isDrawn = true
		harness.fireTimers()

		assert.Empty(t, harness.panel.ReadGuestPane())
		assert.Empty(t, harness.panel.ReadSelectedPanes())
	})
}

func openLanding(t *testing.T) *panelHarness {
	t.Helper()

	harness := openAgents(t, newLiveWorkflow("w1", "api"), newAgent("a1", "old", "w1"))
	harness.press("j")

	return harness
}

func answerAgentCreate(harness *panelHarness, pane string) {
	harness.answer(agentv1connect.AgentServiceCreateAgentProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&agentv1.CreateAgentResponse{Agent: &agentv1.Agent{Id: "a2", PaneId: pane}}), nil
	})
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

// TestListingUnreachable covers the server going away being said once per time it goes.
func TestListingUnreachable(t *testing.T) {
	t.Run("the tree already drawn stays drawn", func(t *testing.T) {
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
