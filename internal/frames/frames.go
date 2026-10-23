package frames

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
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

// AgentDir is the directory that holds one agent's frames, which komrad removes once the agent is gone.
func AgentDir(dir string, agentID uuid.UUID) string {
	return filepath.Join(dir, agentID.String())
}

// Recorder writes an agent's frames under a directory of its own.
type Recorder struct {
	dir      string
	settings func() Settings
	now      func() time.Time

	// mu guards the last frame seen for each agent and the count of what has been written.
	mu      sync.Mutex
	last    map[uuid.UUID]uint64
	written int
}

// NewRecorder builds a recorder writing under dir, reading settings on every record.
func NewRecorder(dir string, settings func() Settings) *Recorder {
	return &Recorder{
		dir:      dir,
		settings: settings,
		now:      time.Now,
		last:     map[uuid.UUID]uint64{},
	}
}

// RecordParams is one reading of one agent.
type RecordParams struct {
	AgentID  uuid.UUID
	Frame    string
	Activity string
	Disputes bool
}

// Record writes the frame an agent was read from when that reading says something new.
func (r *Recorder) Record(params RecordParams) error {
	settings := r.settings()
	if !settings.On || params.AgentID == uuid.Nil || params.Frame == "" {
		return nil
	}

	hash := Hash(params.Frame)
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

// numberFrame numbers a frame within the millisecond it was written, so two readings that land
// together do not share a name.
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

// remember notes the frame an agent was last kept from once the frame is on disk, so komrad takes
// again a frame whose write failed.
func (r *Recorder) remember(agentID uuid.UUID, hash uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.last[agentID] = hash
}

// Hash reduces a frame to the one number held for it between passes.
func Hash(frame string) uint64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(frame))

	return hash.Sum64()
}

// Forget drops what the recorder holds about the agents a listing no longer carries.
func (r *Recorder) Forget(listed map[uuid.UUID]struct{}) {
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

	slices.Sort(written)
	for _, name := range written[:len(written)-keep] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("failed to remove %s: %w", name, err)
		}
	}

	return nil
}
