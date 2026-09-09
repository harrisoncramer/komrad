package agents

import "sync"

// Explainer: the lock is keyed on the directory rather than on the agent because the directory
// is what two operations collide over: a child shares its parent's checkout, and a review round
// is taken of the same tree the agent works in. It holds only within this process, and an
// agent's own CLI does not take it, so it orders weave's operations against each other rather
// than against the thing living in the directory.

// TreeLocks serialises the operations that write to an agent's checkout.
type TreeLocks struct {
	mu   sync.Mutex
	held map[string]struct{}
}

// NewTreeLocks builds an empty set of locks.
func NewTreeLocks() *TreeLocks {
	return &TreeLocks{held: map[string]struct{}{}}
}

// TryLock takes the lock on a directory and hands back what releases it.
func (l *TreeLocks) TryLock(dir string) (func(), bool) {
	if dir == "" {
		return func() {}, true
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
