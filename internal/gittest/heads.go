package gittest

import (
	"context"
	"errors"
	"sync"

	"komrad/internal/git"
)

// Heads answers each directory's head from States, and nothing for any other, recording every
// directory it was asked about.
type Heads struct {
	mu     sync.Mutex
	States map[string]git.HeadState
	Dirs   []string
}

// ReadHeadState records the directory and answers what States holds for it.
func (h *Heads) ReadHeadState(_ context.Context, params git.ReadHeadStateParams) git.HeadState {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Dirs = append(h.Dirs, params.Dir)

	return h.States[params.Dir]
}

// Trees answers what each directory's worktree hashes to and which rounds its base holds, recording every probe and every walk.
type Trees struct {
	mu       sync.Mutex
	Probes   map[string]string
	ProbeErr error
	Probed   []string
	Tips     map[string]string
	Fetched  []string
	Landed   map[string]bool
	Walked   []string
}

// ProbeTree records the root and answers what Probes holds for it, refusing when there are no
// probes.
func (t *Trees) ProbeTree(_ context.Context, root string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Probed = append(t.Probed, root)

	if t.ProbeErr != nil {
		return "", t.ProbeErr
	}
	if t.Probes == nil {
		return "", errors.New("no worktree to probe")
	}

	return t.Probes[root], nil
}

// ReadBaseTip answers what Tips holds for the directory, refusing as a missing base when it
// names none.
func (t *Trees) ReadBaseTip(_ context.Context, dir, _ string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	tip, ok := t.Tips[dir]
	if !ok {
		return "", git.ErrBaseMissing
	}

	return tip, nil
}

// FetchBase records the directory whose base was fetched and answers its origin ref.
func (t *Trees) FetchBase(_ context.Context, dir, base string) (string, string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Fetched = append(t.Fetched, dir)

	return "origin/" + base, "", nil
}

// HasLanded records the round asked about and answers what Landed holds for its snapshot.
func (t *Trees) HasLanded(_ context.Context, params git.HasLandedParams) (bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Walked = append(t.Walked, params.SnapshotSHA)

	return t.Landed[params.SnapshotSHA], nil
}
