// Package frames keeps the pane captures behind an agent's reconciled state, which is the
// evidence a misread frame destroys when the pane redraws over it.
//
// A recording is off unless the config turns it on, since a frame is a few kilobytes and every
// agent is read every pass. What it writes is the capture exactly as the harness read it, so a
// frame can be replayed through the classifier that misjudged it.
package frames

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// stampFormat orders the files in a directory the way the readings happened, since a listing
// sorted by name is then sorted by time.
const stampFormat = "20060102T150405.000"

// Settings are the parts of the config a recording reads, taken fresh so turning one on
// reaches a service already running.
type Settings struct {
	On bool
	// Keep is how many frames an agent holds before the oldest is dropped.
	Keep int
}

// Recorder writes an agent's frames under a directory of its own, keeping the most recent and
// dropping what falls past the limit.
type Recorder struct {
	dir      string
	settings func() Settings
	now      func() time.Time

	// mu guards the last frame seen for each agent and the count of what has been written,
	// both of which the reconcile pass reaches for every agent in turn.
	mu      sync.Mutex
	last    map[string]uint64
	written int
}

// RecordParams is one reading of one agent: the frame it was taken from, what the harness made
// of it, and whether that verdict is one the agent's stored state does not already carry.
type RecordParams struct {
	AgentID  string
	Frame    string
	Activity string
	Disputes bool
}

// NewRecorder builds a recorder writing under dir, reading whether it is on and how much it
// keeps from settings each time it is asked to record.
func NewRecorder(dir string, settings func() Settings) *Recorder {
	return &Recorder{
		dir:      dir,
		settings: settings,
		now:      time.Now,
		last:     map[string]uint64{},
	}
}

// Record writes the frame an agent was read from when that reading says something new, which
// is a frame differing from the one before it or a verdict disputing the state on record.
func (r *Recorder) Record(params RecordParams) error {
	settings := r.settings()
	if !settings.On || params.AgentID == "" || params.Frame == "" {
		return nil
	}

	if !r.isNews(params) {
		return nil
	}

	dir := filepath.Join(r.dir, params.AgentID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}

	name := fmt.Sprintf("%s-%04d-%s.txt", r.now().Format(stampFormat), r.count(), params.Activity)
	if err := os.WriteFile(filepath.Join(dir, name), []byte(params.Frame), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", name, err)
	}

	return prune(dir, settings.Keep)
}

// count numbers a frame within the millisecond it was written in, so two readings that land
// together are both kept rather than one overwriting the other.
func (r *Recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.written++

	return r.written
}

// isNews reports whether a reading is worth keeping, and notes the frame it was taken from so
// the next reading has something to differ from.
func (r *Recorder) isNews(params RecordParams) bool {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(params.Frame))
	sum := hash.Sum64()

	r.mu.Lock()
	defer r.mu.Unlock()

	seen, ok := r.last[params.AgentID]
	r.last[params.AgentID] = sum

	return !ok || sum != seen || params.Disputes
}

// Forget drops what the recorder holds about the agents a listing no longer carries, so a
// deleted agent leaves nothing behind to compare a later one against.
func (r *Recorder) Forget(listed map[string]struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for id := range r.last {
		if _, ok := listed[id]; !ok {
			delete(r.last, id)
		}
	}
}

// prune drops the oldest frames past the number an agent keeps, reading the order off the
// names since they are stamped to sort that way.
func prune(dir string, keep int) error {
	if keep <= 0 {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", dir, err)
	}

	var written []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".txt") {
			written = append(written, entry.Name())
		}
	}

	if len(written) <= keep {
		return nil
	}

	sort.Strings(written)
	for _, name := range written[:len(written)-keep] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("failed to remove %s: %w", name, err)
		}
	}

	return nil
}
