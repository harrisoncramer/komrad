package agent

import (
	"context"
	"testing"
	"time"

	"komrad/internal/store"
	"komrad/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetStateByID pins that a state write finds nothing rather than putting back a row the
// reconciler named after it was deleted, and leaves an archived row as the archive settled it.
func TestSetStateByID(t *testing.T) {
	tests := []struct {
		name      string
		target    string
		archived  bool
		wantErr   error
		wantState State
	}{
		{
			name:      "an id no agent holds",
			target:    "00000000-0000-0000-0000-0000000000ff",
			archived:  false,
			wantErr:   nil,
			wantState: StateIdle,
		},
		{
			name:      "an id that is not an id",
			target:    "not-a-uuid",
			archived:  false,
			wantErr:   store.ErrNotFound,
			wantState: StateIdle,
		},
		{
			name:      "an archived agent",
			target:    "",
			archived:  true,
			wantErr:   nil,
			wantState: StateIdle,
		},
		{
			name:      "the agent's own id",
			target:    "",
			archived:  false,
			wantErr:   nil,
			wantState: StateThinking,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
			created, err := st.Get(ctx, "agent")
			require.NoError(t, err)
			require.NotNil(t, created)
			if tt.archived {
				require.NoError(t, st.archiveByName(ctx, "agent"))
			}

			target := tt.target
			if target == "" {
				target = created.ID
			}

			if tt.wantErr != nil {
				require.ErrorIs(t, st.SetStateByID(ctx, target, StateThinking), tt.wantErr)
			} else {
				require.NoError(t, st.SetStateByID(ctx, target, StateThinking))
			}

			all, err := st.ListAll(ctx)
			require.NoError(t, err)
			require.Len(t, all, 1, "a state write never inserts")
			assert.Equal(t, tt.wantState, all[0].State)
		})
	}
}

// TestSetStateByIDStampsOnlyOnAChange pins that state_changed_at says when the agent
// entered its state rather than when it was last looked at.
func TestSetStateByIDStampsOnlyOnAChange(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, created)

	require.NoError(t, st.SetStateByID(ctx, created.ID, StateThinking))
	moved, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, moved)
	assert.True(t, moved.StateChangedAt.After(created.StateChangedAt), "entering a new state stamps it")

	require.NoError(t, st.SetStateByID(ctx, created.ID, StateThinking))
	again, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, again)
	assert.Equal(t, moved.StateChangedAt, again.StateChangedAt, "observing the same state again leaves it")
}

// TestSettersRefuseAnIDThatIsNotOne pins that the setters a reconcile or resume pass writes
// through say so rather than reporting a write they never made.
func TestSettersRefuseAnIDThatIsNotOne(t *testing.T) {
	tests := []struct {
		name           string
		write          func(*AgentStore, string) error
		refusesMissing bool
	}{
		{
			name: "the cut-off turn",
			write: func(s *AgentStore, id string) error {
				return s.SetInterruptedAtByID(t.Context(), id, xptr.Ptr(time.Now()))
			},
		},
		{
			name:  "the session",
			write: func(s *AgentStore, id string) error { return s.SetSessionID(t.Context(), id, "session") },
		},
		{
			name:           "the reported reason",
			write:          func(s *AgentStore, id string) error { return s.SetReportedReason(t.Context(), id, "Bash") },
			refusesMissing: true,
		},
		{
			name:  "the failed turn",
			write: func(s *AgentStore, id string) error { return s.SetTurnFailed(t.Context(), id, "overloaded") },
		},
		{
			name:  "the hooks",
			write: func(s *AgentStore, id string) error { return s.SetHooksByID(t.Context(), id, []string{"Stop"}) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)

			require.ErrorIs(t, tt.write(st.AgentStore, "not-a-uuid"), store.ErrNotFound)
			missing := tt.write(st.AgentStore, "00000000-0000-0000-0000-0000000000ff")
			if tt.refusesMissing {
				require.ErrorIs(t, missing, store.ErrNotFound)

				return
			}

			require.NoError(t, missing, "an id no agent holds is not the same question")
		})
	}
}

// TestClaimNudge pins when an agent may be told about its mail again: never told, or holding
// unread mail that arrived after it was last told.
func TestClaimNudge(t *testing.T) {
	tests := []struct {
		name    string
		arrange func(t *testing.T, st *fixture, agent *Agent)
		want    bool
	}{
		{
			name: "an agent never told",
			want: true,
		},
		{
			name: "an agent told, with nothing sent since",
			arrange: func(t *testing.T, st *fixture, agent *Agent) {
				claimNudge(t, st, agent.ID)
			},
		},
		{
			name: "an agent told, with mail sent since",
			arrange: func(t *testing.T, st *fixture, agent *Agent) {
				claimNudge(t, st, agent.ID)
				sendMail(t, st, agent.ID, time.Hour)
			},
			want: true,
		},
		{
			name: "an agent told, whose mail sent since was read",
			arrange: func(t *testing.T, st *fixture, agent *Agent) {
				claimNudge(t, st, agent.ID)
				sendMail(t, st, agent.ID, time.Hour)
				_, err := st.db.Exec(t.Context(), `UPDATE mail SET claimed_at = NOW() WHERE to_agent_id = $1`, agent.ID)
				require.NoError(t, err)
			},
		},
		{
			name: "an agent told, whose mail sent since has expired",
			arrange: func(t *testing.T, st *fixture, agent *Agent) {
				claimNudge(t, st, agent.ID)
				sendMail(t, st, agent.ID, -time.Hour)
			},
		},
		{
			name: "an agent whose claim was given back",
			arrange: func(t *testing.T, st *fixture, agent *Agent) {
				claimNudge(t, st, agent.ID)
				require.NoError(t, st.ClearNudge(t.Context(), agent.ID))
			},
			want: true,
		},
		{
			name: "an agent launched again since it was told",
			arrange: func(t *testing.T, st *fixture, agent *Agent) {
				claimNudge(t, st, agent.ID)
				live, err := st.UpdateIfLive(t.Context(), *agent)
				require.NoError(t, err)
				require.True(t, live)
			},
			want: true,
		},
		{
			name: "an archived agent",
			arrange: func(t *testing.T, st *fixture, agent *Agent) {
				_, err := st.Archive(t.Context(), []string{agent.ID})
				require.NoError(t, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			require.NoError(t, st.Create(ctx, Agent{Name: "idler", Pane: "%1", State: StateIdle}))
			agent, err := st.Get(ctx, "idler")
			require.NoError(t, err)
			if tt.arrange != nil {
				tt.arrange(t, st, agent)
			}

			claimed, err := st.ClaimNudge(ctx, agent.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.want, claimed)
		})
	}
}

func claimNudge(t *testing.T, st *fixture, id string) {
	t.Helper()

	claimed, err := st.ClaimNudge(t.Context(), id)
	require.NoError(t, err)
	require.True(t, claimed)
}

func sendMail(t *testing.T, st *fixture, id string, deadline time.Duration) {
	t.Helper()

	_, err := st.db.Exec(t.Context(),
		`INSERT INTO mail (to_agent_id, body, expires_at, engine_id)
		SELECT id, 'please read this', NOW() + $2::interval, engine_id FROM agents WHERE id = $1`,
		id, deadline.String())
	require.NoError(t, err)
}

// TestSetHooksByIDRoundTrips pins that the hooks a launch installed land on the row and that none
// clears them.
func TestSetHooksByIDRoundTrips(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, created)
	require.Empty(t, created.Hooks, "a fresh agent has installed nothing")

	require.NoError(t, st.SetHooksByID(ctx, created.ID, []string{"PreToolUse", "Stop"}))
	installed, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	assert.Equal(t, []string{"PreToolUse", "Stop"}, installed.Hooks)

	require.NoError(t, st.SetHooksByID(ctx, created.ID, nil))
	cleared, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	assert.Empty(t, cleared.Hooks)
}

// TestAFailedTurnStandsUntilAnotherEnds pins that a failure ends the turn it failed and that the
// next turn to end clears it.
func TestAFailedTurnStandsUntilAnotherEnds(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateThinking}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, created)
	require.False(t, created.Turn.IsFailed(), "a fresh agent has failed nothing")

	require.NoError(t, st.SetTurnFailed(ctx, created.ID, "Overloaded"))
	failed, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, failed.Turn.EndedAt)
	assert.True(t, failed.Turn.IsFailed())
	assert.Equal(t, "Overloaded", failed.Turn.Error)

	require.NoError(t, st.SetTurnEnded(ctx, created.ID))
	ended, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	assert.False(t, ended.Turn.IsFailed(), "a later turn ended without an error")
}

// TestSetStateByIDClearsTheReportedReason pins that a reason lives as long as the turn rather
// than the state, and that landing idle takes it off the row.
func TestSetStateByIDClearsTheReportedReason(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateThinking}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, created)
	require.NoError(t, st.SetReportedReason(ctx, created.ID, "Bash"))

	require.NoError(t, st.SetStateByID(ctx, created.ID, StateThinking))
	held, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	assert.Equal(t, "Bash", held.ReportedReason, "the state it annotates has not moved")

	require.NoError(t, st.SetStateByID(ctx, created.ID, StateWaiting))
	blocked, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	assert.Equal(t, "Bash", blocked.ReportedReason,
		"a tool putting a dialog up is the same turn, and the dialog is what the reason names")

	require.NoError(t, st.SetStateByID(ctx, created.ID, StateIdle))
	cleared, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	assert.Empty(t, cleared.ReportedReason)
}

// TestMarkSettingUpClearsTheReportedReason pins that a relaunch drops what the conversation
// before it was doing.
func TestMarkSettingUpClearsTheReportedReason(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateThinking}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, created)
	require.NoError(t, st.SetReportedReason(ctx, created.ID, "Bash"))

	marked, err := st.MarkSettingUpIfLive(ctx, created.ID)
	require.NoError(t, err)
	require.True(t, marked)

	resumed, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	assert.Empty(t, resumed.ReportedReason)
}

// TestARetriedCreateDropsTheFailureReason pins that a retry starts without the old failure.
func TestARetriedCreateDropsTheFailureReason(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateFailedToCreate}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	written, err := st.SetFailureReason(ctx, created.ID, "worktree is dirty")
	require.NoError(t, err)
	require.True(t, written)

	marked, err := st.MarkSettingUpIfFailedToCreate(ctx, created.ID)
	require.NoError(t, err)
	require.True(t, marked)

	retried, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	assert.Empty(t, retried.FailureReason)
}

// TestSetTurnEndedRefusesAnIDThatIsNotOne pins that a malformed id is an error rather than a
// write that silently reaches nothing.
func TestSetTurnEndedRefusesAnIDThatIsNotOne(t *testing.T) {
	st := open(t)

	require.ErrorIs(t, st.SetTurnEnded(t.Context(), "not-a-uuid"), store.ErrNotFound)
}

// TestAliveFollowsThePane pins that every write of the pane writes liveness beside it: a pane
// given makes the row alive, and every write that clears the pane makes it dormant.
func TestAliveFollowsThePane(t *testing.T) {
	tests := []struct {
		name  string
		clear func(ctx context.Context, st *fixture, agent Agent) error
	}{
		{
			name: "an update with no pane",
			clear: func(ctx context.Context, st *fixture, agent Agent) error {
				agent.Pane = ""

				return st.Update(ctx, agent)
			},
		},
		{
			name: "a live update with no pane",
			clear: func(ctx context.Context, st *fixture, agent Agent) error {
				agent.Pane = ""
				_, err := st.UpdateIfLive(ctx, agent)

				return err
			},
		},
		{
			name: "forgetting the pane",
			clear: func(ctx context.Context, st *fixture, agent Agent) error {
				_, err := st.ForgetPane(ctx, ForgetPaneParams{
					ID:        agent.ID,
					Pane:      agent.Pane,
					UpdatedAt: agent.UpdatedAt,
				})

				return err
			},
		},
		{
			name: "marking a resume setting up",
			clear: func(ctx context.Context, st *fixture, agent Agent) error {
				_, err := st.MarkSettingUpIfLive(ctx, agent.ID)

				return err
			},
		},
		{
			name: "marking a retry setting up",
			clear: func(ctx context.Context, st *fixture, agent Agent) error {
				if err := st.SetStateByID(ctx, agent.ID, StateFailedToCreate); err != nil {
					return err
				}
				_, err := st.MarkSettingUpIfFailedToCreate(ctx, agent.ID)

				return err
			},
		},
		{
			name: "archiving",
			clear: func(ctx context.Context, st *fixture, agent Agent) error {
				_, err := st.Archive(ctx, []string{agent.ID})

				return err
			},
		},
		{
			name: "archiving the workflow",
			clear: func(ctx context.Context, st *fixture, agent Agent) error {
				_, err := st.ArchiveWorkflowAgents(ctx, agent.WorkflowID)

				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			require.NoError(t, st.Create(ctx, Agent{Name: "dormant", State: StateIdle}))
			dormant, err := st.Get(ctx, "dormant")
			require.NoError(t, err)
			assert.False(t, dormant.Alive, "a row created with no pane is dormant")

			require.NoError(t, st.Create(ctx, Agent{
				Name:  "agent",
				Pane:  "%1",
				State: StateThinking,
			}))
			created, err := st.Get(ctx, "agent")
			require.NoError(t, err)
			assert.True(t, created.Alive, "a row created with a pane is alive")

			require.NoError(t, tt.clear(ctx, st, *created))

			cleared, err := st.GetByID(ctx, created.ID)
			require.NoError(t, err)
			require.NotNil(t, cleared)
			assert.Empty(t, cleared.Pane)
			assert.False(t, cleared.Alive, "a row whose pane was cleared is dormant")
		})
	}
}

// TestSetSessionID pins that a conversation is recorded only on an agent holding none, and never one
// the agent has already held.
func TestSetSessionID(t *testing.T) {
	tests := []struct {
		name    string
		held    string
		earlier string
		found   string
		want    string
	}{
		{
			name:  "an agent holding no conversation takes the one found",
			found: "session-new",
			want:  "session-new",
		},
		{
			name:  "an agent holding a conversation keeps it",
			held:  "session-held",
			found: "session-new",
			want:  "session-held",
		},
		{
			name:    "a conversation the agent has moved on from is not taken back",
			earlier: "session-earlier",
			found:   "session-earlier",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := open(t)
			ctx := t.Context()

			require.NoError(t, f.Create(ctx, Agent{
				Name:      "agent",
				State:     StateIdle,
				SessionID: tt.held,
			}))
			created, err := f.Get(ctx, "agent")
			require.NoError(t, err)
			if tt.earlier != "" {
				seedSyncedSession(t, f, created.ID, tt.earlier)
			}

			require.NoError(t, f.SetSessionID(ctx, created.ID, tt.found))

			got, err := f.Get(ctx, "agent")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.SessionID)
		})
	}
}
