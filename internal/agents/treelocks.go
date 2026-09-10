package agents

import (
	"context"
	"sync"
	"time"
)

// Explainer: the lock is keyed on the directory rather than on the agent because the directory
// is what two operations collide over: a child shares its parent's checkout, and a review round
// is taken of the same tree the agent works in. It holds only within this process, and an
// agent's own CLI does not take it, so it orders weave's operations against each other rather
// than against the thing living in the directory.

// Explainer: there are two forms because callers differ in what they can do when the lock is
// held. A refresh can refuse and say so, which is TryLock. The delete path cannot: it collects
// the refs holding an agent's snapshots and its only recovery copies and then removes the row,
// so skipping leaks those refs for good with nothing left to trigger a retry. That one waits,
// which is cheap because a refresh holds the tree for seconds.

// TreeLocks serialises the operations that write to an agent's checkout.
type TreeLocks struct {
	mu   sync.Mutex
	held map[string]struct{}
}

// NewTreeLocks builds an empty set of locks.
func NewTreeLocks() *TreeLocks {
	return &TreeLocks{held: map[string]struct{}{}}
}

// lockPoll is how often a waiting caller asks again for a lock somebody else holds.
const lockPoll = 50 * time.Millisecond

// Lock waits for the lock on a directory, giving up when the context is done.
func (l *TreeLocks) Lock(ctx context.Context, dir string) (func(), bool) {
	for {
		if release, taken := l.TryLock(dir); taken {
			return release, true
		}

		select {
		case <-ctx.Done():
			return nil, false
		case <-time.After(lockPoll):
		}
	}
}

// TryLock takes the lock on a directory and hands back what releases it.
func (l *TreeLocks) TryLock(dir string) (func(), bool) {
	// Refused rather than granted, since this is the one place that says no and a caller
	// handing over a blank path would otherwise hold a lock nobody else can see.
	if dir == "" {
		return nil, false
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if _, taken := l.held[dir]; taken {
		return nil, false
	}
	l.held[dir] = struct{}{}

	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		delete(l.held, dir)
	}, true
}
