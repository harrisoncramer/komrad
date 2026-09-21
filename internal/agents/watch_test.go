package agents

import (
	"testing"

	"weave/internal/harness"
	"weave/internal/harness/pane"
	"weave/internal/watchtest"
	"weave/lib/xptr"

	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLaunchPublishesTheAgentItCreated pins the two writes a launch makes on the row.
func TestLaunchPublishesTheAgentItCreated(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManager(tmux, st, "")

	created, err := manager.launch.NewAgent(t.Context(), NewAgentParams{Title: "agent", Dir: t.TempDir(), WorkflowID: seedWorkflow(t, st)})
	require.NoError(t, err)

	assert.Equal(t, []string{created.ID, created.ID}, manager.watch.Agents(), "once as the row is made and once as its pane opens")
}

// TestAFailedLaunchPublishesTheFailedRow pins that a create that gave up says so.
func TestAFailedLaunchPublishesTheFailedRow(t *testing.T) {
	tmux := newFakeTmux()
	tmux.respawnErr = assert.AnError
	st := newStore(t)
	manager := newManager(tmux, st, "")

	_, err := manager.launch.NewAgent(t.Context(), NewAgentParams{Title: "agent", Dir: t.TempDir(), WorkflowID: seedWorkflow(t, st)})
	require.Error(t, err)

	all, err := st.ListAll(t.Context())
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, agentstore.StateFailedToCreate, all[0].State)
	assert.Contains(t, manager.watch.Agents(), all[0].ID)
	assert.Equal(t, all[0].ID, manager.watch.Agents()[len(manager.watch.Agents())-1], "the failure is the last thing said")
}

// TestArchiveAndRestorePublishTheAgent pins both moves between the listings.
func TestArchiveAndRestorePublishTheAgent(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	manager := newManager(tmux, st, "")
	ctx := t.Context()
	agent := seedRunning(t, st, tmux, "target", "%1", agentstore.StateThinking)

	require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, []string{agent.ID}))
	assert.Equal(t, []string{agent.ID}, manager.watch.Agents())

	manager.watch.Reset()
	_, err := manager.lifecycle.RestoreAgents(ctx, []string{agent.ID})
	require.NoError(t, err)
	assert.Equal(t, []string{agent.ID}, manager.watch.Agents())
	assert.Equal(t, []string{agent.WorkflowID}, manager.watch.Workflows(), "the workflow comes back with it")
}

// TestDeletePublishesTheClaimAndTheRemoval pins the two writes a delete makes.
func TestDeletePublishesTheClaimAndTheRemoval(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	manager := newManager(tmux, st, "")
	agent := seedRunning(t, st, tmux, "target", "%1", agentstore.StateThinking)

	require.NoError(t, manager.lifecycle.DeleteAgents(t.Context(), []string{agent.ID}))

	assert.Equal(t, []string{agent.ID, agent.ID}, manager.watch.Agents(), "once as the row is claimed and once as it goes")
}

// TestDeleteRefusedPublishesTheRowsItGaveBack pins that a claim undone is said too.
func TestDeleteRefusedPublishesTheRowsItGaveBack(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	manager := newManager(tmux, st, "")
	agent := seedRunning(t, st, tmux, "target", "%1", agentstore.StateThinking)

	err := manager.lifecycle.DeleteAgents(t.Context(), []string{agent.ID, "019fa434-e7a7-7ac9-bab5-718681da797d"})
	require.ErrorIs(t, err, ErrAgentNotFound)

	assert.Contains(t, manager.watch.Agents(), agent.ID, "a claim handed back is a row read again")
}

// TestResumePublishesTheAgent pins the two writes a resume makes on the row.
func TestResumePublishesTheAgent(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	manager := newManager(tmux, st, "")
	agent := seed(t, st, "dormant", "", agentstore.StateIdle)

	_, err := manager.resume.ResumeAgent(t.Context(), agent.ID)
	require.NoError(t, err)

	assert.Equal(t, []string{agent.ID, agent.ID}, manager.watch.Agents(), "once as it is marked resuming and once as its pane opens")
}

// TestHoldAndReleasePublishTheAgent pins the held stamp coming and going.
func TestHoldAndReleasePublishTheAgent(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "telescope", "%1", agentstore.StateIdle)
	watch := watchtest.NewRecorder()
	channel := &fakeChannel{}
	holder := NewHolder(NewHolderParams{
		Agents:  st.Agents,
		Channel: channel,
		Settler: settlerOver(channel, &settlingObserver{readings: []harness.Activity{harness.ActivityIdle}}),
		Watch:   watch,
	})

	_, err := holder.Hold(t.Context(), agent.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{agent.ID}, watch.Agents())

	_, err = holder.Release(t.Context(), ReleaseParams{AgentID: agent.ID})
	require.NoError(t, err)
	assert.Equal(t, []string{agent.ID, agent.ID}, watch.Agents())
}

// TestRowWriterPublishesEveryWrite pins each write the panel makes on a row.
func TestRowWriterPublishesEveryWrite(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "row", "%1", agentstore.StateIdle)
	watch := watchtest.NewRecorder()
	writer := NewRowWriter(NewRowWriterParams{Agents: st.Agents, Runs: st.Runs, Reconciler: NewReconciler(NewReconcilerParams{}), Watch: watch})
	ctx := t.Context()

	require.NoError(t, writer.SetHidden(ctx, agent.ID, true))
	require.NoError(t, writer.SetMetadata(ctx, SetMetadataParams{AgentID: agent.ID, Key: "branch", Value: xptr.Ptr("agent/x")}))
	require.NoError(t, writer.SetMetadata(ctx, SetMetadataParams{AgentID: agent.ID, Key: "never-set"}), "clearing a key nothing held")
	require.NoError(t, writer.Rename(ctx, agent.ID, "renamed"))
	require.NoError(t, writer.MarkRunsRead(ctx, agent.ID))
	require.NoError(t, writer.RecordReport(ctx, RecordReportParams{AgentID: agent.ID, Reason: "thinking", TurnEnded: true}))

	assert.Equal(t, []string{agent.ID, agent.ID, agent.ID, agent.ID, agent.ID, agent.ID}, watch.Agents(),
		"every write that landed, the cleared key that held nothing aside, and a report with a turn end twice")
}

// TestReconcilePublishesWhatItWroteAndWhatCameOrWent pins the reconciler saying its own writes and a liveness flip, which nothing writes.
func TestReconcilePublishesWhatItWroteAndWhatCameOrWent(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "agent", "%1", agentstore.StateIdle)
	tmux := newFakeTmux()
	tmux.owners["%1"] = agent.ID
	tmux.captures["%1"] = busyFrame
	watch := watchtest.NewRecorder()
	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.NewPane(pane.NewPaneParams{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
		States:   st.Agents,
		Watch:    watch,
	})

	require.NoError(t, rec.Reconcile(t.Context()))
	assert.Equal(t, []string{agent.ID}, watch.Agents(), "the state it wrote")

	watch.Reset()
	require.NoError(t, rec.Reconcile(t.Context()))
	assert.Empty(t, watch.Agents(), "a pass that writes nothing says nothing")

	// The pane goes: the row is forgotten and the liveness flipped, each said once.
	tmux.owners["%1"] = ""
	require.NoError(t, rec.Reconcile(t.Context()))
	assert.Equal(t, []string{agent.ID, agent.ID, agent.ID}, watch.Agents(), "the forgotten pane, the idle state, and the pane gone")
}

// TestWorkflowWritesPublishTheWorkflowAndItsAgents pins that a workflow write reaches the agents whose rows read through it.
func TestWorkflowWritesPublishTheWorkflowAndItsAgents(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	manager := newManager(tmux, st, "")
	workflows, watch := st.workflowServiceWatched(workflowServiceOptions{tmux: tmux})
	ctx := t.Context()

	created, err := workflows.NewWorkflow(ctx, workflowstore.Config{Name: "backend"})
	require.NoError(t, err)
	assert.Equal(t, []string{created.ID}, watch.Workflows())

	agent, err := manager.launch.NewAgent(ctx, NewAgentParams{Title: "agent", Dir: t.TempDir(), WorkflowID: created.ID})
	require.NoError(t, err)

	watch.Reset()
	require.NoError(t, workflows.UpdateWorkflow(ctx, created.ID, workflowstore.Config{Name: "platform", Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"}}))
	assert.Equal(t, []string{created.ID}, watch.Workflows())
	assert.Equal(t, []string{agent.ID}, watch.Agents(), "the base branch is on every agent's row")

	watch.Reset()
	require.NoError(t, workflows.ArchiveWorkflow(ctx, created.ID))
	assert.Equal(t, []string{created.ID}, watch.Workflows())
	assert.Equal(t, []string{agent.ID}, watch.Agents())

	watch.Reset()
	_, err = workflows.RestoreWorkflow(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{created.ID}, watch.Workflows())
	assert.Equal(t, []string{agent.ID}, watch.Agents())
}

// TestDeletingAnEmptiedWorkflowPublishesItAndItsSchedules pins the last delete a workflow gets.
func TestDeletingAnEmptiedWorkflowPublishesItAndItsSchedules(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	manager := newManager(tmux, st, "")
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	schedule := st.SeedSchedule(t, schedulestore.Config{Name: "nightly", Cron: "0 2 * * *", Prompt: "sweep", WorkflowID: workflowID})
	require.NoError(t, st.Schedules.Archive(ctx, schedule.ID))
	require.NoError(t, st.Workflows.Archive(ctx, workflowID))

	require.NoError(t, manager.emptied.DeleteIfEmpty(ctx, workflowID))

	assert.Equal(t, []string{schedule.ID}, manager.watch.Schedules())
	assert.Equal(t, []string{workflowID}, manager.watch.Workflows())
}
