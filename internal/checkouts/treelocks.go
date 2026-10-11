package checkouts

import (
	"path/filepath"
	"sync"
)

// TreeLocks serialises the operations that write to a task's checkout.
type TreeLocks struct {
	mu   sync.Mutex
	held map[string]struct{}
}

// NewTreeLocks builds an empty set of locks.
func NewTreeLocks() *TreeLocks {
	return &TreeLocks{held: map[string]struct{}{}}
}

// IsMoving reports whether a rebase, push or abort holds the checkout at dir.
func (l *TreeLocks) IsMoving(dir string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	_, taken := l.held[filepath.Clean(dir)]

	return taken
}

// TryLock takes the lock on a directory and hands back what releases it.
func (l *TreeLocks) TryLock(dir string) (func(), bool) {
	// Refused rather than granted.
	if dir == "" {
		return nil, false
	}
	dir = filepath.Clean(dir)

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
