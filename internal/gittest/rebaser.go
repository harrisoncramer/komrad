package gittest

import (
	"context"

	"weave/internal/git"
)

// Rebaser answers as a checkout with no repository behind it.
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

func (f *Rebaser) ReadTreeState(context.Context, string) (git.TreeState, error) {
	return f.State, nil
}

func (f *Rebaser) FetchBase(_ context.Context, _, _ string) (ref, sha string, err error) {
	return "origin/main", "basesha", nil
}

func (f *Rebaser) CountCommitsToReplay(context.Context, string, string) (int, error) {
	return f.Commits, nil
}

func (f *Rebaser) PredictRebase(context.Context, string, string) (git.Prediction, error) {
	return f.Prediction, nil
}

func (f *Rebaser) Park(_ context.Context, _, ref string) (string, error) {
	f.Parked = append(f.Parked, ref)
	f.Calls = append(f.Calls, "park")

	return "parkedsha", nil
}

func (f *Rebaser) ReleaseRefsUnder(context.Context, string, string, string) error {
	return nil
}

func (f *Rebaser) Rebase(context.Context, string, string) error {
	f.Calls = append(f.Calls, "rebase")

	return f.ReplayErr
}

func (f *Rebaser) RebaseOnto(_ context.Context, _, _, cutoff string) error {
	f.Cutoff = cutoff
	f.Calls = append(f.Calls, "rebaseOnto")

	return f.ReplayErr
}
