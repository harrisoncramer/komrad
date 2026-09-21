package store

import (
	"errors"
	"testing"

	"weave/internal/dbtest"
	"weave/internal/watchtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openWatched(t *testing.T) (*Pool, *watchtest.Recorder) {
	t.Helper()

	touched := watchtest.NewRecorder()
	pool, err := Open(t.Context(), OpenParams{URL: dbtest.URL(t), Publisher: touched})
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	return pool, touched
}

// TestPoolHandleTouchesAtOnce covers the handle outside any transaction, whose touch reaches
// the publisher as it is made.
func TestPoolHandleTouchesAtOnce(t *testing.T) {
	t.Parallel()

	pool, touched := openWatched(t)

	pool.GetDB().TouchAgents("a1", "a2")
	pool.GetDB().TouchWorkflows("w1")
	pool.GetDB().TouchSchedules("s1")

	assert.Equal(t, []string{"a1", "a2"}, touched.Agents())
	assert.Equal(t, []string{"w1"}, touched.Workflows())
	assert.Equal(t, []string{"s1"}, touched.Schedules())
}

// TestTxTouchesOnCommit covers a transaction: nothing is told while it is open, everything
// touched through it is told once it commits, and a rollback drops the lot.
func TestTxTouchesOnCommit(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool, touched := openWatched(t)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	tx.TouchAgents("a1")
	tx.TouchWorkflows("w1")
	assert.Empty(t, touched.Agents(), "an open transaction has told nobody")

	require.NoError(t, tx.Commit(ctx))
	assert.Equal(t, []string{"a1"}, touched.Agents())
	assert.Equal(t, []string{"w1"}, touched.Workflows())

	touched.Reset()
	dropped, err := pool.Begin(ctx)
	require.NoError(t, err)
	dropped.TouchAgents("a2")
	require.NoError(t, dropped.Rollback(ctx))
	assert.Empty(t, touched.Agents(), "a rolled-back transaction drops its touches")
}

// TestInTxTouchesAfterCommit covers InTx, whose touches wait for the commit and go nowhere
// when fn fails.
func TestInTxTouchesAfterCommit(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool, touched := openWatched(t)

	require.NoError(t, pool.InTx(ctx, func(db DBTX) error {
		db.TouchSchedules("s1")
		assert.Empty(t, touched.Schedules(), "the touch waits for the commit")

		return nil
	}))
	assert.Equal(t, []string{"s1"}, touched.Schedules())

	failed := errors.New("no")
	err := pool.InTx(ctx, func(db DBTX) error {
		db.TouchSchedules("s2")

		return failed
	})
	require.ErrorIs(t, err, failed)
	assert.Equal(t, []string{"s1"}, touched.Schedules(), "a failed fn's touches are dropped")
}

// TestNestedTxTouchesReachTheOuterCommit covers a transaction begun inside another, whose
// touches are handed up on its commit and told only when the outermost one commits.
func TestNestedTxTouchesReachTheOuterCommit(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool, touched := openWatched(t)

	outer, err := pool.Begin(ctx)
	require.NoError(t, err)
	inner, err := outer.Begin(ctx)
	require.NoError(t, err)
	inner.TouchAgents("a1")
	require.NoError(t, inner.Commit(ctx))
	assert.Empty(t, touched.Agents(), "the inner commit hands the touch to the outer transaction")

	require.NoError(t, outer.Commit(ctx))
	assert.Equal(t, []string{"a1"}, touched.Agents())
}

// TestUnwatchedTellsNobody covers the handle over a bare connection, whose touches go nowhere
// and which opens no transaction.
func TestUnwatchedTellsNobody(t *testing.T) {
	t.Parallel()

	pool, touched := openWatched(t)

	db := Unwatched(pool.GetRawPool())
	db.TouchAgents("a1")
	assert.Empty(t, touched.Agents())
}
