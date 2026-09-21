// Package watchtest records what a writer published, for a test to assert on.
package watchtest

import (
	"slices"
	"sync"
)

// Recorder keeps every id touched, by kind, in the order it was touched.
type Recorder struct {
	mu        sync.Mutex
	agents    []string
	workflows []string
	schedules []string
}

// NewRecorder builds a recorder holding nothing.
func NewRecorder() *Recorder {
	return &Recorder{}
}

// TouchAgents records the agent ids.
func (r *Recorder) TouchAgents(ids ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.agents = append(r.agents, ids...)
}

// TouchWorkflows records the workflow ids.
func (r *Recorder) TouchWorkflows(ids ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.workflows = append(r.workflows, ids...)
}

// TouchSchedules records the schedule ids.
func (r *Recorder) TouchSchedules(ids ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.schedules = append(r.schedules, ids...)
}

// Agents is every agent id touched so far.
func (r *Recorder) Agents() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.agents)
}

// Workflows is every workflow id touched so far.
func (r *Recorder) Workflows() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.workflows)
}

// Schedules is every schedule id touched so far.
func (r *Recorder) Schedules() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.schedules)
}

// Reset forgets everything touched so far.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.agents, r.workflows, r.schedules = nil, nil, nil
}
