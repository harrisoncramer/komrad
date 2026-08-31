package agent

import (
	"testing"
	"time"

	"weave/internal/store"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetStateByID pins that a state write finds nothing rather than putting back a row the
// reconciler named after it was deleted.
func TestSetStateByID(t *testing.T) {
	tests := []struct {
		name string

		target string

		wantErr   error
		wantState string
	}{
		{
			name:      "an id no agent holds",
			target:    "00000000-0000-0000-0000-0000000000ff",
			wantState: StateIdle,
		},
		{
			name:      "an id that is not an id",
			target:    "not-a-uuid",
			wantErr:   store.ErrNotFound,
			wantState: StateIdle,
		},
		{
			name:      "the agent's own id",
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

// TestSetStateByIDKeepsTheTag pins that a state write leaves the tag alone, since the tag
// belongs to the user and the reconciler writes state constantly.
func TestSetStateByIDKeepsTheTag(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, created)

	tagged, err := st.SetTags(ctx, []string{created.ID}, xptr.Ptr("●"))
	require.NoError(t, err)
	require.Equal(t, 1, tagged)

	require.NoError(t, st.SetStateByID(ctx, created.ID, StateWaiting))

	after, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, after)
	assert.Equal(t, StateWaiting, after.State)
	assert.Equal(t, "●", *after.Tag, "a state write leaves the user's tag standing")
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
		write func(*Store, string) error
	}{
		{
			name:  "the branch",
			write: func(s *Store, id string) error { return s.SetBranchByID(t.Context(), id, xptr.Ptr("main")) },
		},
		{
			name:  "the removed worktree",
			write: func(s *Store, id string) error { return s.SetWorktreeRemovedByID(t.Context(), id, true) },
		},
		{
			name:  "the cut-off turn",
			write: func(s *Store, id string) error { return s.SetInterruptedAtByID(t.Context(), id, xptr.Ptr(time.Now())) },
		},
		{
			name:  "the session",
			write: func(s *Store, id string) error { return s.SetSessionID(t.Context(), id, "session") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)

			require.ErrorIs(t, tt.write(st.Store, "not-a-uuid"), store.ErrNotFound)
			assert.NoError(t, tt.write(st.Store, "00000000-0000-0000-0000-0000000000ff"),
				"an id no agent holds is not the same question")
		})
	}
}

// TestANudgeIsHeldForTheRetryInterval pins that a nudge just taken stands while one left
// standing longer than the interval can be taken again.
func TestANudgeIsHeldForTheRetryInterval(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "idler", Pane: "%1", State: StateIdle}))
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
// clock, since inside a transaction NOW() holds at the start while the caller's clock runs on.
func TestANudgeIsReclaimedOnTheDatabaseClock(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "idler", Pane: "%1", State: StateIdle}))
	agent, err := st.Get(ctx, "idler")
	require.NoError(t, err)

	claimed, err := st.ClaimNudge(ctx, agent.ID, time.Minute)
	require.NoError(t, err)
	require.True(t, claimed)

	require.NoError(t, st.pool.InTx(ctx, func(db store.DBTX) error {
		time.Sleep(300 * time.Millisecond)

		again, err := New(db).ClaimNudge(ctx, agent.ID, 200*time.Millisecond)
		require.NoError(t, err)
		assert.False(t, again, "no database time has passed, so the nudge still stands")

		return nil
	}))
}

func backdateNudge(t *testing.T, st *fixture, id string, age time.Duration) {
	t.Helper()

	_, err := st.db.Exec(t.Context(),
		`UPDATE agents SET nudged_at = NOW() - $2::interval WHERE id = $1`, id, age.String())
	require.NoError(t, err)
}
