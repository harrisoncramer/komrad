package agents

import (
	"context"
	"testing"

	"weave/config"
	agentstore "weave/internal/store/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newNotifier builds a real message service over a test's own store, the same type the
// server wires in as the agent service's notifier.
func newNotifier(st *testStore) *MessageService {
	return NewMessageService(NewMessageServiceParams{
		MessageReadWriter: st.Messages,
		AgentFinder:       st.Agents,
		DeadlineReader:    fakeDeadline{},
		HopLimitReader:    fakeHopLimit(config.DefaultMaxHops),
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
				child := seedSpawned(t, st, parent.ID, "child", "%child", agentstore.StateThinking)
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

			pending, err := st.Messages.CountPending(ctx, parent.ID)
			require.NoError(t, err)
			if tt.wantMessage {
				assert.EqualValues(t, 1, pending)
			} else {
				assert.Zero(t, pending)
			}
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
				child := seedSpawned(t, st, parent.ID, "child", "%child", agentstore.StateThinking)
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

			pending, err := st.Messages.CountPending(ctx, parentID)
			require.NoError(t, err)
			assert.EqualValues(t, 1, pending)
		})
	}
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
			child := seedSpawned(t, st, parent.ID, "child", "%child", agentstore.StateThinking)

			for range maxPendingMessages {
				_, err := notifier.SendMessage(ctx, SendMessageParams{ToAgentID: parent.ID, Body: "filler"})
				require.NoError(t, err)
			}

			require.NoError(t, tt.action(manager, ctx, child.ID))

			pending, err := st.Messages.CountPending(ctx, parent.ID)
			require.NoError(t, err)
			assert.EqualValues(t, maxPendingMessages, pending, "a refused notice is not retried into a stray extra message")
		})
	}
}
