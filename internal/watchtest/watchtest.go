// Package watchtest records what a writer published, for a test to assert on.
package watchtest

import (
	"slices"
	"sync"
)

// Recorder keeps every id touched, by table, in the order it was touched.
type Recorder struct {
	mu      sync.Mutex
	touched map[string][]string
}

// NewRecorder builds a recorder holding nothing.
func NewRecorder() *Recorder {
	return &Recorder{touched: map[string][]string{}}
}

// Touch records the ids under their table.
func (r *Recorder) Touch(table string, ids ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.touched[table] = append(r.touched[table], ids...)
}

// Agents is every agent id touched so far.
func (r *Recorder) Agents() []string {
	return r.of("agents")
}

// Workflows is every workflow id touched so far.
func (r *Recorder) Workflows() []string {
	return r.of("workflows")
}

// Schedules is every schedule id touched so far.
func (r *Recorder) Schedules() []string {
	return r.of("schedules")
}

func (r *Recorder) of(table string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.touched[table])
}

// Reset forgets everything touched so far.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	clear(r.touched)
}
