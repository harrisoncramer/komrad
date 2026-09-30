package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"komrad/internal/store"
	"komrad/lib/clock"
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
		name  string
		write func(*AgentStore, string) error
	}{
		{
			name: "the checkout",
			write: func(s *AgentStore, id string) error {
				return s.SetCheckout(t.Context(), id, SetCheckoutParams{Branch: xptr.Ptr("main")})
			},
		},
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
			name:  "the reported reason",
			write: func(s *AgentStore, id string) error { return s.SetReportedReason(t.Context(), id, "Bash") },
		},
		{
			name:  "the failed turn",
			write: func(s *AgentStore, id string) error { return s.SetTurnFailed(t.Context(), id, "overloaded") },
		},
		{
			name: "the last rebase",
			write: func(s *AgentStore, id string) error {
				return s.SetLastRebaseByID(t.Context(), id, json.RawMessage(`{}`))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)

			require.ErrorIs(t, tt.write(st.AgentStore, "not-a-uuid"), store.ErrNotFound)
			require.NoError(t, tt.write(st.AgentStore, "00000000-0000-0000-0000-0000000000ff"),
				"an id no agent holds is not the same question")
		})
	}
}

// TestANudgeIsHeldForTheRetryInterval pins that a nudge just taken stands while one left
// standing longer than the interval can be taken again.
func TestANudgeIsHeldForTheRetryInterval(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{
		Name:  "idler",
		Pane:  "%1",
		State: StateIdle,
	}))
	agent, err := st.Get(ctx, "idler")
	require.NoError(t, err)

	claimed, err := st.ClaimNudge(ctx, agent.ID, time.Minute)
	require.NoError(t, err)
	require.True(t, claimed, "an agent never nudged is there to be nudged")

	again, err := st.ClaimNudge(ctx, agent.ID, time.Minute)
	require.NoError(t, err)
	assert.False(t, again, "and not again while the one just taken still stands")

	backdateNudge(t, st, agent.ID, 2*time.Minute)

	again, err = st.ClaimNudge(ctx, agent.ID, time.Minute)
	require.NoError(t, err)
	assert.True(t, again, "a nudge older than the interval is spent")
}

// TestANudgeIsReclaimedOnTheDatabaseClock pins that the interval is counted on the database
// clock, where NOW() holds at the start of a transaction while the caller's clock runs on.
func TestANudgeIsReclaimedOnTheDatabaseClock(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{
		Name:  "idler",
		Pane:  "%1",
		State: StateIdle,
	}))
	agent, err := st.Get(ctx, "idler")
	require.NoError(t, err)

	claimed, err := st.ClaimNudge(ctx, agent.ID, time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)

	require.NoError(t, st.pool.InTx(ctx, func(db store.DBTX) error {
		clock.Sleep(t.Context(), 300*time.Millisecond)

		again, err := NewAgentStore(db).ClaimNudge(ctx, agent.ID, 200*time.Millisecond)
		require.NoError(t, err)
		assert.False(t, again, "no database time has passed, so the nudge still stands")

		return nil
	}))
}

// TestAClaimedNudgeIsNotASentOne pins that the stamp the claim takes says nothing about
// whether an agent was ever told, which only a pass that typed something records.
func TestAClaimedNudgeIsNotASentOne(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{
		Name:  "idler",
		Pane:  "%1",
		State: StateIdle,
	}))
	agent, err := st.Get(ctx, "idler")
	require.NoError(t, err)

	claimed, err := st.ClaimNudge(ctx, agent.ID, time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)

	agent, err = st.Get(ctx, "idler")
	require.NoError(t, err)
	require.NotNil(t, agent.NudgedAt, "the claim is stamped")
	assert.Nil(t, agent.NudgeSentAt, "and nothing has been typed at the pane yet")

	require.NoError(t, st.SetNudgeSent(ctx, agent.ID))

	agent, err = st.Get(ctx, "idler")
	require.NoError(t, err)
	require.NotNil(t, agent.NudgeSentAt)

	again, err := st.ClaimNudge(ctx, agent.ID, time.Minute)
	require.NoError(t, err)
	assert.False(t, again, "recording the nudge leaves the claim where it was")
}

func backdateNudge(t *testing.T, st *fixture, id string, age time.Duration) {
	t.Helper()

	_, err := st.db.Exec(t.Context(),
		`UPDATE agents SET nudged_at = NOW() - $2::interval WHERE id = $1`, id, age.String())
	require.NoError(t, err)
}

// TestSetReportedReasonRoundTrips pins that what an agent's CLI reported lands on the row and that
// an empty reason clears it.
func TestSetReportedReasonRoundTrips(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateThinking}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, created)
	require.Empty(t, created.ReportedReason, "a fresh agent has reported nothing")

	require.NoError(t, st.SetReportedReason(ctx, created.ID, "Bash"))
	reported, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	assert.Equal(t, "Bash", reported.ReportedReason)

	require.NoError(t, st.SetReportedReason(ctx, created.ID, ""))
	cleared, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	assert.Empty(t, cleared.ReportedReason)
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
	require.False(t, created.IsLastTurnFailed(), "a fresh agent has failed nothing")

	require.NoError(t, st.SetTurnFailed(ctx, created.ID, "Overloaded"))
	failed, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, failed.TurnEndedAt)
	assert.True(t, failed.IsLastTurnFailed())
	assert.Equal(t, "Overloaded", failed.TurnError)

	require.NoError(t, st.SetTurnEnded(ctx, created.ID))
	ended, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	assert.False(t, ended.IsLastTurnFailed(), "a later turn ended without an error")
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

// TestSetAlive covers the reconciler's write: the named rows and no other, either way.
func TestSetAlive(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	for _, name := range []string{"a", "b", "c"} {
		require.NoError(t, st.Create(ctx, Agent{
			Name:  name,
			Pane:  "%" + name,
			State: StateIdle,
		}))
	}
	a, err := st.Get(ctx, "a")
	require.NoError(t, err)
	b, err := st.Get(ctx, "b")
	require.NoError(t, err)

	require.NoError(t, st.SetAlive(ctx, []string{a.ID, b.ID, "not-a-uuid"}, false))

	alive := map[string]bool{}
	for _, name := range []string{"a", "b", "c"} {
		agent, err := st.Get(ctx, name)
		require.NoError(t, err)
		alive[name] = agent.Alive
	}
	assert.Equal(t, map[string]bool{"a": false, "b": false, "c": true}, alive)

	require.NoError(t, st.SetAlive(ctx, []string{a.ID}, true))
	a, err = st.Get(ctx, "a")
	require.NoError(t, err)
	assert.True(t, a.Alive)

	require.NoError(t, st.SetAlive(ctx, nil, true), "no ids is nothing to write")
}

// TestLastRebaseRoundTrips pins an agent's last rebase reading back as written, nil before its first.
func TestLastRebaseRoundTrips(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)

	never, err := st.GetLastRebaseByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Nil(t, never)

	require.NoError(t, st.SetLastRebaseByID(ctx, created.ID, json.RawMessage(`{"failure": "first"}`)))
	require.NoError(t, st.SetLastRebaseByID(ctx, created.ID, json.RawMessage(`{"failure": "second"}`)))
	last, err := st.GetLastRebaseByID(ctx, created.ID)
	require.NoError(t, err)
	assert.JSONEq(t, `{"failure": "second"}`, string(last))

	_, err = st.GetLastRebaseByID(ctx, "00000000-0000-0000-0000-0000000000ff")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestHoldSettlesTheState pins that a hold records a state the pane decides as idle and leaves a
// failure as it was.
func TestHoldSettlesTheState(t *testing.T) {
	tests := []struct {
		name         string
		state        State
		wantState    State
		wantReason   string
		wantTurnEnds bool
	}{
		{
			name:         "a working agent is held idle and its turn ends",
			state:        StateThinking,
			wantState:    StateIdle,
			wantTurnEnds: true,
		},
		{
			name:         "a waiting agent is held idle and its turn ends",
			state:        StateWaiting,
			wantState:    StateIdle,
			wantTurnEnds: true,
		},
		{
			name:      "an idle agent keeps the turn it last ended",
			state:     StateIdle,
			wantState: StateIdle,
		},
		{
			name:       "a failed agent keeps its failure",
			state:      StateFailedToCleanup,
			wantState:  StateFailedToCleanup,
			wantReason: "Bash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: tt.state}))
			created, err := st.Get(ctx, "agent")
			require.NoError(t, err)
			require.NotNil(t, created)
			require.NoError(t, st.SetReportedReason(ctx, created.ID, "Bash"))

			held, err := st.Hold(ctx, HoldParams{
				ID:      created.ID,
				At:      time.Now(),
				MidTurn: tt.wantTurnEnds,
			})
			require.NoError(t, err)
			require.True(t, held)

			after, err := st.Get(ctx, "agent")
			require.NoError(t, err)
			require.NotNil(t, after)
			assert.NotNil(t, after.HeldAt)
			assert.Equal(t, tt.wantState, after.State)
			assert.Equal(t, tt.wantReason, after.ReportedReason)
			assert.Equal(t, tt.wantTurnEnds, after.TurnEndedAt != nil)
		})
	}
}
