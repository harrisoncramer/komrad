package agents

import "sync"

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
