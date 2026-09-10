package agents

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTreeLocksLockWaitsForTheHolder covers the waiting form, which the delete path needs
// because it cannot come back later.
func TestTreeLocksLockWaitsForTheHolder(t *testing.T) {
	t.Parallel()

	locks := NewTreeLocks()
	release, taken := locks.TryLock("/tree")
	require.True(t, taken)

	go func() {
		time.Sleep(50 * time.Millisecond)
		release()
	}()

	second, taken := locks.Lock(t.Context(), "/tree")
	require.True(t, taken, "the wait ended with the lock rather than without it")
	second()
}

// TestTreeLocksLockGivesUpWhenTheContextDoes pins the wait being bounded, since a caller that
// blocked for ever on a stuck holder would be worse than one that gave up.
func TestTreeLocksLockGivesUpWhenTheContextDoes(t *testing.T) {
	t.Parallel()

	locks := NewTreeLocks()
	release, taken := locks.TryLock("/tree")
	require.True(t, taken)
	defer release()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, taken = locks.Lock(ctx, "/tree")
	assert.False(t, taken)
}
