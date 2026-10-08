package checkouts

import "sync"

// treeLocks serialises the operations that write to an agent's checkout.
type treeLocks struct {
	mu   sync.Mutex
	held map[string]struct{}
}

// newTreeLocks builds an empty set of locks.
func newTreeLocks() *treeLocks {
	return &treeLocks{held: map[string]struct{}{}}
}

// tryLock takes the lock on a directory and hands back what releases it.
func (l *treeLocks) tryLock(dir string) (func(), bool) {
	// Refused rather than granted.
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
