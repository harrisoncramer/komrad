package ui_test

import (
	"errors"
	"testing"

	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestListingLandsWrites covers the cursor going to the row a write made once the watch carries it.
func TestListingLandsWrites(t *testing.T) {
	t.Run("a created agent is waited for and then landed on", func(t *testing.T) {
		harness := openLanding(t)
		answerAgentCreate(harness, "")

		harness.press("c", "h", "i", "ctrl+s")
		require.Equal(t, "  old", readHoveredRow(harness.readFrame()), "the write puts nothing on the list and the cursor waits")

		harness.watch(buildPuts(newAgent("a2", "new", "w1")))

		assert.Equal(t, "  new", readHoveredRow(harness.readFrame()))
	})

	t.Run("a created workflow is landed on once its header arrives", func(t *testing.T) {
		harness := openLanding(t)
		answerWorkflowCreate(harness, "w2")

		harness.press("w", "w", "e", "b", "ctrl+s")
		harness.watch(buildPuts(newLiveWorkflow("w2", "web")))

		assert.Equal(t, "web", readHoveredRow(harness.readFrame()))
	})

	t.Run("a row the watch delivered before the write answered is landed on at once", func(t *testing.T) {
		harness := openLanding(t)
		answerAgentCreate(harness, "")
		harness.watch(buildPuts(newAgent("a2", "new", "w1")))
		require.Equal(t, "  old", readHoveredRow(harness.readFrame()))

		harness.press("c", "h", "i", "ctrl+s")

		assert.Equal(t, "  new", readHoveredRow(harness.readFrame()))
	})

	t.Run("a refused create lands nowhere", func(t *testing.T) {
		harness := openLanding(t)
		harness.refuse(agentv1connect.AgentServiceCreateAgentProcedure, connect.NewError(connect.CodeInvalidArgument, errors.New("not allowed")))

		harness.press("c", "h", "i", "ctrl+s", "esc")
		harness.watch(buildPuts(newAgent("a2", "new", "w1")))

		assert.Equal(t, "  old", readHoveredRow(harness.readFrame()))
	})

	t.Run("a key pressed while the landing waits takes the cursor back", func(t *testing.T) {
		harness := openLanding(t)
		answerAgentCreate(harness, "")

		harness.press("c", "h", "i", "ctrl+s", "k")
		harness.watch(buildPuts(newAgent("a2", "new", "w1")))

		assert.Equal(t, "api", readHoveredRow(harness.readFrame()))
	})

	t.Run("a row the filter hides leaves the cursor on a row it can see", func(t *testing.T) {
		harness := openLanding(t)
		answerAgentCreate(harness, "")

		harness.press("/", "tab", "^", "o", "l", "d", "$", "enter")
		harness.press("c", "h", "i", "ctrl+s")
		harness.watch(buildPuts(newAgent("a2", "new", "w1")))

		assert.Equal(t, "  old", readHoveredRow(harness.readFrame()))
	})

	t.Run("a batch for another row keeps the pane the write showed", func(t *testing.T) {
		old := newAgent("a1", "old", "w1")
		old.PaneId = "%1"
		old.Alive = proto.Bool(true)
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
		created.Alive = proto.Bool(true)
		harness.watch(buildPuts(created))

		assert.Equal(t, "  new", readHoveredRow(harness.readFrame()))
		assert.Equal(t, "%9", harness.panel.ReadGuestPane())
	})

	t.Run("a write with nothing to do with an open log pane leaves it up", func(t *testing.T) {
		harness := openLanding(t)
		harness.refuse(agentv1connect.AgentServiceUpdateAgentProcedure, connect.NewError(connect.CodeAlreadyExists, errors.New("name is taken")))
		harness.press("l")
		opened := len(harness.panel.ReadRespawns())
		require.Positive(t, opened, "the log is paged into a pane")

		harness.press("r", "x", "ctrl+s", "esc")

		assert.Empty(t, harness.panel.ReadKilledPanes(), "the log's pane is left where it is")
	})

	t.Run("the archive cursor keeps its agent when a folder above it goes", func(t *testing.T) {
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
	t.Run("the open key over a staged agent hands it the keys", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "api"), newRunning())
		harness.press("j")
		harness.fireTimers()

		harness.press("enter")

		assert.Equal(t, []string{"%9"}, harness.panel.ReadSelectedPanes())
	})

	t.Run("the open key over an agent the stage could not show leaves the keys", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{refuseSplits: true})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "api"), newRunning()))

		harness.press("j")
		harness.fireTimers()
		harness.press("enter")

		assert.Empty(t, harness.panel.ReadSelectedPanes())
	})

	t.Run("a resume hands the keys to the pane it puts back", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "api"), newDormant("old"))
		answerResume(harness)

		harness.press("j", "enter")

		assert.Equal(t, "%9", harness.panel.ReadGuestPane())
		assert.Equal(t, []string{"%9"}, harness.panel.ReadSelectedPanes())
	})

	t.Run("a create stages its pane and leaves the keys on the panel", func(t *testing.T) {
		harness := openLanding(t)
		answerAgentCreate(harness, "%9")

		harness.press("c", "h", "i", "ctrl+s")

		assert.Equal(t, "%9", harness.panel.ReadGuestPane())
		assert.Empty(t, harness.panel.ReadSelectedPanes())
	})

	t.Run("a resume waits for the harness to draw before handing over the keys", func(t *testing.T) {
		frames := &drawnFrames{}
		harness := newPanelHarness(t, panelHarnessParams{harnessFrames: frames})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "api"), newDormant("old")))
		answerResume(harness)

		harness.press("j", "enter")
		require.Empty(t, harness.panel.ReadSelectedPanes(), "a pane still at its shell is not given the keys")

		frames.drawn = true
		harness.fireTimers()

		assert.Equal(t, "%9", harness.panel.ReadGuestPane())
		assert.Equal(t, []string{"%9"}, harness.panel.ReadSelectedPanes())
	})

	t.Run("a resume tmux will not split for leaves the keys on the panel", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{refuseSplits: true})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "api"), newDormant("old")))
		answerResume(harness)

		harness.press("j", "enter")

		assert.Empty(t, harness.panel.ReadGuestPane())
		assert.Empty(t, harness.panel.ReadSelectedPanes(), "the keys stay on the panel rather than moving to a pane nobody can see")
	})

	t.Run("a resume tmux will not split for once the harness draws leaves the keys too", func(t *testing.T) {
		frames := &drawnFrames{}
		harness := newPanelHarness(t, panelHarnessParams{harnessFrames: frames, refuseSplits: true})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "api"), newDormant("old")))
		answerResume(harness)

		harness.press("j", "enter")
		frames.drawn = true
		harness.fireTimers()

		assert.Empty(t, harness.panel.ReadGuestPane())
		assert.Empty(t, harness.panel.ReadSelectedPanes())
	})
}

type drawnFrames struct {
	drawn bool
}

func (f *drawnFrames) DoesFrameShowHarness(string, string) bool {
	return f.drawn
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
	agent.Alive = proto.Bool(true)

	return agent
}
