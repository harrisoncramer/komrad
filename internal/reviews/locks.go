package reviews

import "sync"

// agentLocks serialises the work done for one agent at a time, leaving other agents free.
type agentLocks struct {
	mu   sync.Mutex
	held map[string]*agentLock
}

// agentLock is one agent's lock and how many callers are holding or waiting on it.
type agentLock struct {
	mu      sync.Mutex
	waiters int
}

func newAgentLocks() *agentLocks {
	return &agentLocks{held: map[string]*agentLock{}}
}

// lock waits for the agent's lock and hands back what releases it.
func (l *agentLocks) lock(agentID string) func() {
	l.mu.Lock()
	held, ok := l.held[agentID]
	if !ok {
		held = &agentLock{}
		l.held[agentID] = held
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
			delete(l.held, agentID)
		}
	}
}
