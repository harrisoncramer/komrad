package usage

import (
	"testing"
	"time"

	"weave/internal/pgtest"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSumSinceWindowsWhatWasReported is the whole reason the counts are stored a row per
// exchange rather than as a running total on the agent, so it covers a span that leaves some
// of an agent's own exchanges out.
func TestSumSinceWindowsWhatWasReported(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	busy := seedAgent(t, st, "busy")
	quiet := seedAgent(t, st, "quiet")

	now := time.Now()
	require.NoError(t, st.Append(ctx, busy, []Reported{
		{Key: "old", At: now.Add(-9 * time.Hour), Input: 1000, CacheRead: 2000, CacheWrite: 3000, Output: 4000},
		{Key: "recent", At: now.Add(-time.Hour), Input: 2, CacheRead: 40, CacheWrite: 300, Output: 7},
		{Key: "newer", At: now.Add(-time.Minute), Input: 3, CacheRead: 60, CacheWrite: 100, Output: 8},
	}))
	require.NoError(t, st.Append(ctx, quiet, []Reported{
		{Key: "long ago", At: now.Add(-9 * time.Hour), Input: 5, CacheRead: 5, CacheWrite: 5, Output: 5},
	}))

	totals, err := st.SumSince(ctx, now.Add(-4*time.Hour))
	require.NoError(t, err)

	assert.Equal(t, []Totals{{
		AgentID:    busy,
		Input:      5,
		CacheRead:  100,
		CacheWrite: 400,
		Output:     15,
	}}, totals, "an agent that reported nothing in the window is absent rather than zero")
	assert.Equal(t, int64(520), totals[0].Tokens())
}

// TestAppendCountsAnExchangeOnce covers a log read twice, which is what a restart does, since
// the same exchange arriving again must not double the reading.
func TestAppendCountsAnExchangeOnce(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	id := seedAgent(t, st, "agent")

	at := time.Now().Add(-time.Minute)
	reported := []Reported{{Key: "msg_1", At: at, Input: 2, CacheRead: 40, CacheWrite: 300, Output: 7}}

	require.NoError(t, st.Append(ctx, id, reported))
	require.NoError(t, st.Append(ctx, id, reported))

	totals, err := st.SumSince(ctx, at.Add(-time.Minute))
	require.NoError(t, err)
	assert.Equal(t, []Totals{{
		AgentID:    id,
		Input:      2,
		CacheRead:  40,
		CacheWrite: 300,
		Output:     7,
	}}, totals)
}

// TestAppendCountsARepeatedKeyInOneBatchOnce covers the same key twice in a single write, which
// is the shape a harness hands over when one reply is written a line per content block.
func TestAppendCountsARepeatedKeyInOneBatchOnce(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	id := seedAgent(t, st, "agent")

	at := time.Now().Add(-time.Minute)
	require.NoError(t, st.Append(ctx, id, []Reported{
		{Key: "msg_1", At: at, Output: 7},
		{Key: "msg_1", At: at, Output: 7},
	}))

	totals, err := st.SumSince(ctx, at.Add(-time.Minute))
	require.NoError(t, err)
	assert.Equal(t, []Totals{{AgentID: id, Output: 7}}, totals)
}

// TestAppendIgnoresWhatItCannotStore covers the calls the syncer makes on every pass for an
// agent whose log said nothing.
func TestAppendIgnoresWhatItCannotStore(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	id := seedAgent(t, st, "agent")

	require.NoError(t, st.Append(ctx, id, nil))
	require.NoError(t, st.Append(ctx, "not-an-id", []Reported{{Key: "msg_1", At: time.Now()}}))

	totals, err := st.SumSince(ctx, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	assert.Empty(t, totals)
}

// TestPruneDropsWhatIsPastRetention keeps the table bounded, which is what lets an exchange be
// stored a row at a time in the first place.
func TestPruneDropsWhatIsPastRetention(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	id := seedAgent(t, st, "agent")

	now := time.Now()
	require.NoError(t, st.Append(ctx, id, []Reported{
		{Key: "swept", At: now.Add(-Retention - time.Hour), Output: 100},
		{Key: "kept", At: now.Add(-time.Hour), Output: 7},
	}))

	require.NoError(t, st.Prune(ctx))

	totals, err := st.SumSince(ctx, now.Add(-Retention))
	require.NoError(t, err)
	assert.Equal(t, []Totals{{AgentID: id, Output: 7}}, totals)
}

// TestDeletingAnAgentTakesItsUsage covers the rows going with the agent, since nothing else
// sweeps what an agent reported before it was removed.
func TestDeletingAnAgentTakesItsUsage(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	id := seedAgent(t, st, "agent")

	now := time.Now()
	require.NoError(t, st.Append(ctx, id, []Reported{{Key: "msg_1", At: now.Add(-time.Minute), Output: 7}}))
	_, err := st.agents.Delete(ctx, []string{id})
	require.NoError(t, err)

	totals, err := st.SumSince(ctx, now.Add(-time.Hour))
	require.NoError(t, err)
	assert.Empty(t, totals)
}

type fixture struct {
	*Store
	agents   *agentstore.Store
	workflow string
}

func open(t *testing.T) *fixture {
	t.Helper()
	p, err := store.Open(t.Context(), pgtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	workflows := workflowstore.New(p.GetDB())
	created, err := workflows.Create(t.Context(), workflowstore.Config{Name: "default"})
	require.NoError(t, err)

	return &fixture{
		Store:    New(p.GetDB()),
		agents:   agentstore.New(p.GetDB()),
		workflow: created.ID,
	}
}

func seedAgent(t *testing.T, st *fixture, name string) string {
	t.Helper()
	ctx := t.Context()
	require.NoError(t, st.agents.Create(ctx, agentstore.Agent{
		Name: name, State: agentstore.StateIdle, WorkflowID: st.workflow,
	}))
	got, err := st.agents.GetInWorkflow(ctx, st.workflow, name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return got.ID
}
