// Package frames keeps the pane captures behind an agent's reconciled state.
//
// A pane redraws over the frame a wrong state was read from, so without a recording the only
// account of a misread left is the code. A recording is off unless the config turns it on, and
// what it writes is the capture exactly as the harness read it, so a frame can be replayed
// through the classifier that misjudged it.
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

// stampFormat names a frame so that sorting a listing by name sorts it by time, in UTC so a
// clock going back cannot sort a new frame before an old one.
const stampFormat = "20060102T150405.000"

// Settings are the parts of the config a recording reads.
type Settings struct {
	On bool
	// Keep is how many frames an agent holds before the oldest is dropped.
	Keep int
}

// AgentDir is where one agent's frames are kept, and what has to be removed once it is gone.
func AgentDir(dir, agentID string) string {
	return filepath.Join(dir, agentID)
}

// Recorder writes an agent's frames under a directory of its own.
type Recorder struct {
	dir      string
	settings func() Settings
	now      func() time.Time

	// mu guards the last frame seen for each agent and the count of what has been written.
	mu      sync.Mutex
	last    map[string]uint64
	written int
}

// RecordParams is one reading of one agent.
type RecordParams struct {
	AgentID  string
	Frame    string
	Activity string
	Disputes bool
}

// NewRecorder builds a recorder writing under dir, reading settings on every record.
func NewRecorder(dir string, settings func() Settings) *Recorder {
	return &Recorder{
		dir:      dir,
		settings: settings,
		now:      time.Now,
		last:     map[string]uint64{},
	}
}

// Record writes the frame an agent was read from when that reading says something new.
func (r *Recorder) Record(params RecordParams) error {
	settings := r.settings()
	if !settings.On || params.AgentID == "" || params.Frame == "" {
		return nil
	}

	hash := hashFrame(params.Frame)
	if !r.shouldRecord(params, hash) {
		return nil
	}

	dir := AgentDir(r.dir, params.AgentID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}

	name := fmt.Sprintf("%s-%04d-%s.txt", r.now().UTC().Format(stampFormat), r.numberFrame(), params.Activity)
	if err := os.WriteFile(filepath.Join(dir, name), []byte(params.Frame), 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", name, err)
	}
	r.remember(params.AgentID, hash)

	return prune(dir, settings.Keep)
}

// numberFrame numbers a frame within the millisecond it was written in, keeping two readings
// that land together off the same name.
func (r *Recorder) numberFrame() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.written++

	return r.written
}

// shouldRecord reports whether a reading is worth keeping, which a frame differing from the last
// one kept and a verdict disputing the state on record both are.
func (r *Recorder) shouldRecord(params RecordParams, hash uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	seen, ok := r.last[params.AgentID]

	return !ok || hash != seen || params.Disputes
}

// remember notes the frame an agent was last kept from, once it is on disk, so a write that
// failed is taken again.
func (r *Recorder) remember(agentID string, hash uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.last[agentID] = hash
}

// hashFrame reduces a frame to the one number held for it between passes.
func hashFrame(frame string) uint64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(frame))

	return hash.Sum64()
}

// Forget drops what the recorder holds about the agents a listing no longer carries.
func (r *Recorder) Forget(listed map[string]struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for id := range r.last {
		if _, ok := listed[id]; !ok {
			delete(r.last, id)
		}
	}
}

// prune drops the oldest frames past the number an agent keeps.
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
