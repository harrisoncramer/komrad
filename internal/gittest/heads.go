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
	Err    error
	Dirs   []string
}

// ReadHeadState records the directory and answers what States holds for it, or Err.
func (h *Heads) ReadHeadState(_ context.Context, params git.ReadHeadStateParams) (*git.HeadState, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Dirs = append(h.Dirs, params.Dir)

	if h.Err != nil {
		return nil, h.Err
	}

	state := h.States[params.Dir]

	return &state, nil
}

// Trees answers what each directory's worktree hashes to and which rounds its base holds, recording every probe and every walk.
type Trees struct {
	mu       sync.Mutex
	Probes   map[string]string
	ProbeErr error
	Probed   []string
	Tips     map[string]string
	// Unresolved says the clone holds no copy of the base.
	Unresolved bool
	// BasesRead is every directory whose base was read.
	BasesRead []string
	Fetched   []string
	FetchErr  error
	Landed    map[string]bool
	Walked    []string
	// DiffStats is what each directory measures against its base.
	DiffStats map[string]git.DiffStat
	DiffErr   error
	Measured  []git.ReadDiffStatParams
	// Changes is what every directory holds uncommitted.
	Changes git.Changes
	// Prediction is what a rebase of every directory would do.
	Prediction git.Prediction
	Predicted  int
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

// ReadBase records the directory and answers origin's copy of the base at what Tips holds for the
// directory, refusing as a missing base when Unresolved says the clone has none.
func (t *Trees) ReadBase(_ context.Context, dir, base string) (*git.Base, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.BasesRead = append(t.BasesRead, dir)

	if t.Unresolved {
		return nil, git.ErrBaseMissing
	}

	return &git.Base{Ref: "origin/" + base, Tip: t.Tips[dir]}, nil
}

// FetchBase records the directory whose base was fetched and answers its origin ref, or FetchErr.
func (t *Trees) FetchBase(_ context.Context, dir, base string) (string, string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Fetched = append(t.Fetched, dir)

	if t.FetchErr != nil {
		return "", "", t.FetchErr
	}

	return "origin/" + base, "", nil
}

// HasLanded records the round asked about and answers what Landed holds for its snapshot.
func (t *Trees) HasLanded(_ context.Context, params git.HasLandedParams) (bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Walked = append(t.Walked, params.SnapshotSHA)

	return t.Landed[params.SnapshotSHA], nil
}

// ReadDiffStat records what was measured and answers what DiffStats holds for the directory,
// refusing as unrelated when it names none.
func (t *Trees) ReadDiffStat(_ context.Context, params git.ReadDiffStatParams) (*git.DiffStat, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Measured = append(t.Measured, params)

	if t.DiffErr != nil {
		return nil, t.DiffErr
	}

	stat, ok := t.DiffStats[params.Dir]
	if !ok {
		return nil, git.ErrBaseUnrelated
	}

	return &stat, nil
}

// ReadChanges answers Changes.
func (t *Trees) ReadChanges(context.Context, string) (git.Changes, error) {
	return t.Changes, nil
}

// PeekChanges answers Changes.
func (t *Trees) PeekChanges(context.Context, string) (git.Changes, error) {
	return t.Changes, nil
}

// ReadTreeState answers that nothing is part-way through.
func (t *Trees) ReadTreeState(context.Context, string) (git.TreeState, error) {
	return git.TreeState{}, nil
}

// IsSequenceInProgress answers that nothing is part-way through.
func (t *Trees) IsSequenceInProgress(context.Context, string) (bool, error) {
	return false, nil
}

// PredictRebase counts the prediction and answers Prediction.
func (t *Trees) PredictRebase(context.Context, string, string) (git.Prediction, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Predicted++

	return t.Prediction, nil
}
