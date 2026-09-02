package agents

import (
	"context"
	"testing"

	"weave/config"
	"weave/internal/caller"
	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newNotifier(st *testStore) *MailService {
	return NewMailService(NewMailServiceParams{
		MailReadWriter: st.Mail,
		AgentFinder:    st.Agents,
		Config:         fakeMailConfig{maxHops: config.DefaultMaxHops},
	})
}

func newManagerWithNotifier(tmux *fakeTmux, st *testStore, notifier systemMailSender) *testAgents {
	return st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: fakeHarnesses{harness: &fakeHarness{}},
		addr:      "127.0.0.1:9999",
		notifier:  notifier,
	})
}

// TestArchiveTellsAParentItsDelegateIsGone pins when archiving mails the parent, so a parent
// archived in the same batch is not written to.
func TestArchiveTellsAParentItsDelegateIsGone(t *testing.T) {
	tests := []struct {
		name             string
		hasParent        bool
		archiveParentToo bool
		wantMail         bool
	}{
		{
			name: "an agent with no parent tells nobody",
		},
		{
			name:             "a parent archived in the same batch hears nothing",
			hasParent:        true,
			archiveParentToo: true,
		},
		{
			name:      "a live parent hears its delegate was archived",
			hasParent: true,
			wantMail:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := newFakeTmux()
			st := newStore(t)
			manager := newManagerWithNotifier(tmux, st, newNotifier(st))
			ctx := t.Context()

			var parent agentstore.Agent
			var ids []string
			if tt.hasParent {
				parent = seedRunning(t, st, tmux, "parent", "%parent", agentstore.StateIdle)
				child := seedSpawned(t, st, parent.ID)
				ids = append(ids, child.ID)
				if tt.archiveParentToo {
					ids = append(ids, parent.ID)
				}
			} else {
				child := seedRunning(t, st, tmux, "child", "%child", agentstore.StateThinking)
				ids = append(ids, child.ID)
			}

			require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, ids))

			if !tt.hasParent {
				return
			}

			claimed, err := st.Mail.Claim(ctx, parent.ID, 10)
			require.NoError(t, err)
			if !tt.wantMail {
				assert.Empty(t, claimed)

				return
			}

			require.Len(t, claimed, 1)
			assert.Equal(t, WeaveSenderName, claimed[0].FromName, "a lifecycle notice reads as weave's own voice")
			assert.Nil(t, claimed[0].FromAgentID)
			assert.Contains(t, claimed[0].Body, "archived")
		})
	}
}

// TestDeleteTellsAParentItsDelegateIsGone pins when deleting mails the parent, so a parent
// deleted in the same batch is not written to.
func TestDeleteTellsAParentItsDelegateIsGone(t *testing.T) {
	tests := []struct {
		name            string
		hasParent       bool
		deleteParentToo bool
		wantMail        bool
	}{
		{
			name: "an agent with no parent tells nobody",
		},
		{
			name:            "a parent deleted in the same batch hears nothing",
			hasParent:       true,
			deleteParentToo: true,
		},
		{
			name:      "a live parent hears its delegate was deleted",
			hasParent: true,
			wantMail:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := newFakeTmux()
			st := newStore(t)
			manager := newManagerWithNotifier(tmux, st, newNotifier(st))
			ctx := t.Context()

			var parentID string
			var ids []string
			if tt.hasParent {
				parent := seedRunning(t, st, tmux, "parent", "%parent", agentstore.StateIdle)
				parentID = parent.ID
				child := seedSpawned(t, st, parent.ID)
				ids = append(ids, child.ID)
				if tt.deleteParentToo {
					ids = append(ids, parent.ID)
				}
			} else {
				child := seedRunning(t, st, tmux, "child", "%child", agentstore.StateThinking)
				ids = append(ids, child.ID)
			}

			require.NoError(t, manager.lifecycle.DeleteAgents(ctx, ids))

			if !tt.hasParent || tt.deleteParentToo {
				return
			}

			claimed, err := st.Mail.Claim(ctx, parentID, 10)
			require.NoError(t, err)
			require.Len(t, claimed, 1)
			assert.Equal(t, WeaveSenderName, claimed[0].FromName, "a lifecycle notice reads as weave's own voice")
			assert.Nil(t, claimed[0].FromAgentID)
			assert.Contains(t, claimed[0].Body, "deleted")
		})
	}
}

// TestDeleteOfAFamilyTellsNobodyAcrossTheSplit pins the batch a cleanup action splits: the
// parent is held back for its own cleanup while its blank-action child goes straight through,
// and the child's news is still nobody's to read.
func TestDeleteOfAFamilyTellsNobodyAcrossTheSplit(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	notifier := &recordingNotifier{}
	manager := st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: fakeHarnesses{harness: &fakeHarness{}},
		addr:      "127.0.0.1:9999",
		lifecycle: &fakeLifecycle{},
		notifier:  notifier,
	})
	ctx := t.Context()

	workflow, err := st.Workflows.Create(ctx, worktreeConfig(t, "worktrees"))
	require.NoError(t, err)

	parent := seedInWorkflow(t, st, workflow.ID, "author", "%author", agentstore.StateIdle)
	require.NoError(t, tmux.SetPaneOwner(parent.Pane, parent.ID))
	seedChild(t, st, tmux, parent)

	require.NoError(t, manager.lifecycle.DeleteAgents(ctx, []string{parent.ID}))

	assert.Empty(t, notifier.to, "a parent on its way out is not told about the child going with it")
}

// TestDeleteOfASpawnerPairTellsNobody pins the other side of that split: two agents in a
// worktree workflow are both held back for their own cleanup, so each callback has to weigh
// its mail against the whole call rather than against the one agent it is finishing.
func TestDeleteOfASpawnerPairTellsNobody(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	notifier := &recordingNotifier{}
	manager := st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: fakeHarnesses{harness: &fakeHarness{}},
		addr:      "127.0.0.1:9999",
		lifecycle: &fakeLifecycle{},
		notifier:  notifier,
	})
	ctx := t.Context()

	workflow, err := st.Workflows.Create(ctx, worktreeConfig(t, "worktrees"))
	require.NoError(t, err)

	orchestrator := seedInWorkflow(t, st, workflow.ID, "orchestrator", "%orchestrator", agentstore.StateIdle)
	require.NoError(t, tmux.SetPaneOwner(orchestrator.Pane, orchestrator.ID))

	require.NoError(t, st.Create(ctx, agentstore.Agent{
		Name:             "worker",
		Pane:             "%worker",
		State:            agentstore.StateThinking,
		WorkflowID:       workflow.ID,
		SpawnedByAgentID: &orchestrator.ID,
	}))

	worker, err := st.Agents.GetInWorkflow(ctx, workflow.ID, "worker")
	require.NoError(t, err)
	require.NotNil(t, worker)
	require.NoError(t, tmux.SetPaneOwner(worker.Pane, worker.ID))

	require.NoError(t, manager.lifecycle.DeleteAgents(ctx, []string{orchestrator.ID, worker.ID}))

	assert.Empty(t, notifier.to, "a spawner deleted alongside its delegate is not written to")
}

// recordingNotifier keeps who was written to, since a delete takes the row the mail was
// addressed to and the mailbox cannot be read afterwards.
type recordingNotifier struct {
	to []string
}

func (n *recordingNotifier) SendSystemMail(_ context.Context, toAgentID, _ string) (*mailstore.Mail, error) {
	n.to = append(n.to, toAgentID)

	return &mailstore.Mail{ToAgentID: toAgentID}, nil
}

// TestNotifyParentsSkipsTheActorsOwnDelegate pins that a parent is not mailed news it made
// itself, since reading it would spend a turn for nothing.
func TestNotifyParentsSkipsTheActorsOwnDelegate(t *testing.T) {
	tests := []struct {
		name     string
		actor    func(parentID, otherID string) string
		wantMail bool
	}{
		{
			name:     "the parent archiving its own delegate hears nothing",
			actor:    func(parentID, _ string) string { return parentID },
			wantMail: false,
		},
		{
			name:     "the panel operator archiving the same delegate still tells the parent",
			actor:    func(_, _ string) string { return "" },
			wantMail: true,
		},
		{
			name:     "another agent archiving somebody else's delegate still tells the parent",
			actor:    func(_, otherID string) string { return otherID },
			wantMail: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := newFakeTmux()
			st := newStore(t)
			manager := newManagerWithNotifier(tmux, st, newNotifier(st))

			parent := seedRunning(t, st, tmux, "parent", "%parent", agentstore.StateIdle)
			other := seedRunning(t, st, tmux, "other", "%other", agentstore.StateIdle)
			child := seedSpawned(t, st, parent.ID)

			ctx := caller.With(t.Context(), caller.Caller{AgentID: tt.actor(parent.ID, other.ID)})
			require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, []string{child.ID}))

			claimed, err := st.Mail.Claim(t.Context(), parent.ID, 10)
			require.NoError(t, err)
			if tt.wantMail {
				assert.Len(t, claimed, 1)
			} else {
				assert.Empty(t, claimed)
			}
		})
	}
}

// TestArchiveWorkflowTellsAParentOutsideIt covers the cross-workflow case: create_agent
// takes a workflowId, so a parent can spawn a delegate into a workflow of its own, and
// archiving that workflow must not strand the parent with no word its delegate is gone,
// even though ArchiveAgents is never called.
func TestArchiveWorkflowTellsAParentOutsideIt(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	workflows := st.workflowServiceWithNotifier(tmux, 0, newNotifier(st))
	ctx := t.Context()

	parent := seedRunning(t, st, tmux, "parent", "%parent", agentstore.StateIdle)
	childWorkflow := st.SeedWorkflow(t, workflowstore.Config{Name: "delegate-work"})
	child := seedSpawned(t, st, parent.ID)
	require.NoError(t, tmux.SetPaneOwner("%child", child.ID))
	child.WorkflowID = childWorkflow.ID
	require.NoError(t, st.Agents.Update(ctx, child))

	require.NoError(t, workflows.ArchiveWorkflow(ctx, childWorkflow.ID))

	claimed, err := st.Mail.Claim(ctx, parent.ID, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	assert.Equal(t, WeaveSenderName, claimed[0].FromName)
	assert.Contains(t, claimed[0].Body, "archived")
}

// TestNotifyParentDoesNotBlockOnAFullMailbox pins that a parent's full mailbox does not undo
// the archive or delete somebody asked for.
func TestNotifyParentDoesNotBlockOnAFullMailbox(t *testing.T) {
	tests := []struct {
		name   string
		action func(lifecycle *LifecycleService, ctx context.Context, id string) error
	}{
		{
			name: "archive",
			action: func(lifecycle *LifecycleService, ctx context.Context, id string) error {
				return lifecycle.ArchiveAgents(ctx, []string{id})
			},
		},
		{
			name: "delete",
			action: func(lifecycle *LifecycleService, ctx context.Context, id string) error {
				return lifecycle.DeleteAgents(ctx, []string{id})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := newFakeTmux()
			st := newStore(t)
			notifier := newNotifier(st)
			manager := newManagerWithNotifier(tmux, st, notifier)
			ctx := t.Context()

			parent := seedRunning(t, st, tmux, "parent", "%parent", agentstore.StateIdle)
			child := seedSpawned(t, st, parent.ID)

			for range maxPendingMail {
				_, err := notifier.SendSystemMail(ctx, parent.ID, "filler")
				require.NoError(t, err)
			}

			require.NoError(t, tt.action(manager.lifecycle, ctx, child.ID))

			pending, err := st.Mail.CountPending(ctx, parent.ID)
			require.NoError(t, err)
			assert.EqualValues(t, maxPendingMail, pending, "a refused notice is not retried into a stray extra message")
		})
	}
}
