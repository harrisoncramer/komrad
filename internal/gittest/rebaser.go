package gittest

import (
	"context"

	"weave/internal/git"
)

// Rebaser is the git a rebase drives, answering from its fields and recording each call.
type Rebaser struct {
	State      git.TreeState
	Prediction git.Prediction
	Commits    int
	Parked     []string
	Cutoff     string
	// ReplayErr is what a replay answers with, which is how a conflict is asked for.
	ReplayErr error
	// Calls is what was asked of git, in the order it was asked.
	Calls []string
}

// ReadTreeState answers State.
func (f *Rebaser) ReadTreeState(context.Context, string) (git.TreeState, error) {
	return f.State, nil
}

// FetchBase answers origin/main.
func (f *Rebaser) FetchBase(_ context.Context, _, _ string) (ref, sha string, err error) {
	return "origin/main", "basesha", nil
}

// CountCommitsToReplay answers Commits.
func (f *Rebaser) CountCommitsToReplay(context.Context, string, string) (int, error) {
	return f.Commits, nil
}

// PredictRebase answers Prediction.
func (f *Rebaser) PredictRebase(context.Context, string, string) (git.Prediction, error) {
	return f.Prediction, nil
}

// Park records the ref it parks.
func (f *Rebaser) Park(_ context.Context, _, ref string) (string, error) {
	f.Parked = append(f.Parked, ref)
	f.Calls = append(f.Calls, "park")

	return "parkedsha", nil
}

// ReleaseRefsUnder does nothing.
func (f *Rebaser) ReleaseRefsUnder(context.Context, string, string, string) error {
	return nil
}

// Rebase records the call and answers ReplayErr.
func (f *Rebaser) Rebase(context.Context, string, string) error {
	f.Calls = append(f.Calls, "rebase")

	return f.ReplayErr
}

// RebaseOnto records the cutoff and answers ReplayErr.
func (f *Rebaser) RebaseOnto(_ context.Context, _, _, cutoff string) error {
	f.Cutoff = cutoff
	f.Calls = append(f.Calls, "rebaseOnto")

	return f.ReplayErr
}
