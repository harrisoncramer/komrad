package reviews

import "sync"

// taskLocks serialises the work done for one task at a time, leaving other tasks free.
type taskLocks struct {
	mu   sync.Mutex
	held map[string]*taskLock
}

// taskLock is one task's lock and how many callers are holding or waiting on it.
type taskLock struct {
	mu      sync.Mutex
	waiters int
}

func newTaskLocks() *taskLocks {
	return &taskLocks{held: map[string]*taskLock{}}
}

// lock waits for the task's lock and hands back what releases it.
func (l *taskLocks) lock(taskID string) func() {
	l.mu.Lock()
	held, ok := l.held[taskID]
	if !ok {
		held = &taskLock{}
		l.held[taskID] = held
	}
	held.waiters++
	l.mu.Unlock()

	held.mu.Lock()

	return func() {
		held.mu.Unlock()

		l.mu.Lock()
		defer l.mu.Unlock()
		held.waiters--
		if held.waiters == 0 {
			delete(l.held, taskID)
		}
	}
}
