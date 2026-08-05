package agents

import (
	"context"
	"testing"

	"weave/config"
	"weave/internal/caller"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newNotifier builds a real message service over a test's own store, the same type the
// server wires in as the agent service's notifier.
func newNotifier(st *testStore) *MessageService {
	return NewMessageService(NewMessageServiceParams{
		MessageReadWriter: st.Messages,
		Agents:            st.Agents,
		Config:            fakeMessageConfig{maxHops: config.DefaultMaxHops},
	})
}

func newManagerWithNotifier(tmux *fakeTmux, st *testStore, notifier parentNotifier) *AgentService {
	return st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: fakeHarnesses{harness: &fakeHarness{}},
		addr:      "127.0.0.1:9999",
		notifier:  notifier,
	})
}

func TestArchiveTellsAParentItsDelegateIsGone(t *testing.T) {
	tests := []struct {
		name             string
		hasParent        bool
		archiveParentToo bool
		wantMessage      bool
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
			name:        "a live parent hears its delegate was archived",
			hasParent:   true,
			wantMessage: true,
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

			require.NoError(t, manager.ArchiveAgents(ctx, ids))

			if !tt.hasParent {
				return
			}

			claimed, err := st.Messages.Claim(ctx, parent.ID, 10)
			require.NoError(t, err)
			if !tt.wantMessage {
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

func TestDeleteTellsAParentItsDelegateIsGone(t *testing.T) {
	tests := []struct {
		name            string
		hasParent       bool
		deleteParentToo bool
		wantMessage     bool
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
			name:        "a live parent hears its delegate was deleted",
			hasParent:   true,
			wantMessage: true,
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

			require.NoError(t, manager.DeleteAgents(ctx, ids))

			if !tt.hasParent || tt.deleteParentToo {
				return
			}

			claimed, err := st.Messages.Claim(ctx, parentID, 10)
			require.NoError(t, err)
			require.Len(t, claimed, 1)
			assert.Equal(t, WeaveSenderName, claimed[0].FromName, "a lifecycle notice reads as weave's own voice")
			assert.Nil(t, claimed[0].FromAgentID)
			assert.Contains(t, claimed[0].Body, "deleted")
		})
	}
}

// TestNotifyParentsSkipsTheActorsOwnDelegate covers the noise case: a parent that archived
// or deleted its own delegate already knows, since it just did it, so mailing it back would
// spend a turn reading news it made happen. Anyone else acting on that delegate, including
// the panel operator, still produces the notice.
func TestNotifyParentsSkipsTheActorsOwnDelegate(t *testing.T) {
	tests := []struct {
		name        string
		actor       func(parentID, otherID string) string
		wantMessage bool
	}{
		{
			name:        "the parent archiving its own delegate hears nothing",
			actor:       func(parentID, _ string) string { return parentID },
			wantMessage: false,
		},
		{
			name:        "the panel operator archiving the same delegate still tells the parent",
			actor:       func(_, _ string) string { return "" },
			wantMessage: true,
		},
		{
			name:        "another agent archiving somebody else's delegate still tells the parent",
			actor:       func(_, otherID string) string { return otherID },
			wantMessage: true,
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
			require.NoError(t, manager.ArchiveAgents(ctx, []string{child.ID}))

			claimed, err := st.Messages.Claim(t.Context(), parent.ID, 10)
			require.NoError(t, err)
			if tt.wantMessage {
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

	claimed, err := st.Messages.Claim(ctx, parent.ID, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	assert.Equal(t, WeaveSenderName, claimed[0].FromName)
	assert.Contains(t, claimed[0].Body, "archived")
}

// TestNotifyParentDoesNotBlockOnAFullMailbox covers the failure path: a person asked for
// an agent to go away, and a parent's mailbox refusing the news is not a reason to undo
// that. The failure is logged, not surfaced, and nothing sneaks a message in around it.
func TestNotifyParentDoesNotBlockOnAFullMailbox(t *testing.T) {
	tests := []struct {
		name   string
		action func(manager *AgentService, ctx context.Context, id string) error
	}{
		{
			name: "archive",
			action: func(manager *AgentService, ctx context.Context, id string) error {
				return manager.ArchiveAgents(ctx, []string{id})
			},
		},
		{
			name: "delete",
			action: func(manager *AgentService, ctx context.Context, id string) error {
				return manager.DeleteAgents(ctx, []string{id})
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

			for range maxPendingMessages {
				_, err := notifier.SendSystemMessage(ctx, parent.ID, "filler")
				require.NoError(t, err)
			}

			require.NoError(t, tt.action(manager, ctx, child.ID))

			pending, err := st.Messages.CountPending(ctx, parent.ID)
			require.NoError(t, err)
			assert.EqualValues(t, maxPendingMessages, pending, "a refused notice is not retried into a stray extra message")
		})
	}
}
