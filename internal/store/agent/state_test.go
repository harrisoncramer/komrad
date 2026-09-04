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
		name      string
		target    string
		wantErr   error
		wantState State
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
		{
			name:  "the reported reason",
			write: func(s *Store, id string) error { return s.SetReportedReason(t.Context(), id, "Bash") },
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
// clock, since inside a transaction NOW() holds at the start while the caller's clock runs on.
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

// TestListUnseenTurnsFollowsTheReportedTurn covers the whole point of taking the mark off what
// the agent's own CLI reported: an agent is unread because it said a turn of its ended, and
// nothing weave thought it saw on a pane can say otherwise.
func TestListUnseenTurnsFollowsTheReportedTurn(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)

	unseen, err := st.ListUnseenTurns(ctx)
	require.NoError(t, err)
	assert.Empty(t, unseen, "an agent that has ended no turn is not unread")

	require.NoError(t, st.SetStateByID(ctx, created.ID, StateThinking))
	unseen, err = st.ListUnseenTurns(ctx)
	require.NoError(t, err)
	assert.Empty(t, unseen, "and working is not finishing")

	require.NoError(t, st.SetTurnEnded(ctx, created.ID))
	require.NoError(t, st.SetStateByID(ctx, created.ID, StateIdle))
	unseen, err = st.ListUnseenTurns(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{created.ID}, unseen)

	require.NoError(t, st.SetViewed(ctx, created.ID, true))
	unseen, err = st.ListUnseenTurns(ctx)
	require.NoError(t, err)
	assert.Empty(t, unseen, "and read once somebody has looked")

	require.NoError(t, st.SetTurnEnded(ctx, created.ID))
	unseen, err = st.ListUnseenTurns(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{created.ID}, unseen, "a turn ending after the look is unread again")
}

// TestListUnseenTurnsDropsAnAgentBackAtWork covers an agent that finished, was never read, and
// then went again on its own, where the mark stood for a turn nobody is waiting on any more.
func TestListUnseenTurnsDropsAnAgentBackAtWork(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NoError(t, st.SetTurnEnded(ctx, created.ID))

	unseen, err := st.ListUnseenTurns(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{created.ID}, unseen)

	require.NoError(t, st.SetStateByID(ctx, created.ID, StateThinking))
	unseen, err = st.ListUnseenTurns(ctx)
	require.NoError(t, err)
	assert.Empty(t, unseen, "a row that went again is not waiting on a reader")

	require.NoError(t, st.SetTurnEnded(ctx, created.ID))
	require.NoError(t, st.SetStateByID(ctx, created.ID, StateIdle))
	unseen, err = st.ListUnseenTurns(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{created.ID}, unseen, "and the turn it went on to end is unread")
}

// TestListUnseenTurnsKeepsAnAgentBlockedOnAPerson pins that only working takes the mark off,
// since a row stopped on a prompt is the one most worth travelling to.
func TestListUnseenTurnsKeepsAnAgentBlockedOnAPerson(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NoError(t, st.SetTurnEnded(ctx, created.ID))
	require.NoError(t, st.SetStateByID(ctx, created.ID, StateWaiting))

	unseen, err := st.ListUnseenTurns(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{created.ID}, unseen)
}

// TestSetViewedStampsTheTurnItRead pins that the mark records the turn weave had been told
// about rather than the clock, so a turn landing mid-look is not read along with the look.
func TestSetViewedStampsTheTurnItRead(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NoError(t, st.SetTurnEnded(ctx, created.ID))

	ended, err := st.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, ended.TurnEndedAt)

	require.NoError(t, st.SetViewed(ctx, created.ID, true))
	read, err := st.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, read.ViewedAt)
	assert.Equal(t, ended.TurnEndedAt.UTC(), read.ViewedAt.UTC())
}

// TestSetViewedPutsTheMarkBack covers the manual half, where somebody says they have not read
// what weave decided they had.
func TestSetViewedPutsTheMarkBack(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NoError(t, st.SetTurnEnded(ctx, created.ID))
	require.NoError(t, st.SetViewed(ctx, created.ID, true))

	require.NoError(t, st.SetViewed(ctx, created.ID, false))
	back, err := st.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Nil(t, back.ViewedAt)

	unseen, err := st.ListUnseenTurns(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{created.ID}, unseen)
}

// TestSetViewedOnAnAgentThatHasEndedNoTurn pins that claiming an agent weave has heard nothing
// from records nothing, so the first turn it does finish is still news.
func TestSetViewedOnAnAgentThatHasEndedNoTurn(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NoError(t, st.SetViewed(ctx, created.ID, true))

	require.NoError(t, st.SetTurnEnded(ctx, created.ID))
	unseen, err := st.ListUnseenTurns(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{created.ID}, unseen)
}

// TestListUnseenTurnsLeavesTheArchiveAlone pins that archiving an agent takes it out of the
// mark, since an archived row has no place on the list the mark is drawn on.
func TestListUnseenTurnsLeavesTheArchiveAlone(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NoError(t, st.SetTurnEnded(ctx, created.ID))

	_, err = st.Archive(ctx, []string{created.ID})
	require.NoError(t, err)

	unseen, err := st.ListUnseenTurns(ctx)
	require.NoError(t, err)
	assert.Empty(t, unseen)
}

// TestSetReportedReasonRoundTrips pins that what an agent's CLI reported lands on the row and
// that an empty reason clears it, which is what a turn ending reports.
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

// TestSetStateByIDClearsTheReportedReason pins that a reason lives exactly as long as the state
// it annotates, which is what keeps a tool an agent never finished off the row of an agent that
// has since stopped, and how a reason a restart lost the timestamp for still goes.
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

	require.NoError(t, st.SetStateByID(ctx, created.ID, StateIdle))
	cleared, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	assert.Empty(t, cleared.ReportedReason)
}

// TestMarkSettingUpClearsTheReportedReason pins that a relaunch drops what the conversation
// before it was doing, since the CLI that reported it is gone.
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

// TestSetTurnEndedRefusesAnIDThatIsNotOne pins that a malformed id is an error rather than a
// write that silently reaches nothing.
func TestSetTurnEndedRefusesAnIDThatIsNotOne(t *testing.T) {
	st := open(t)

	require.ErrorIs(t, st.SetTurnEnded(t.Context(), "not-a-uuid"), store.ErrNotFound)
}
