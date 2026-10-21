package gittest

import (
	"context"
	"slices"

	"komrad/internal/git"
)

// Mover is the git a rebase, push or abort drives, answering from its fields and recording each call.
type Mover struct {
	State      git.TreeState
	Prediction git.Prediction
	Commits    int
	Parked     []string
	Cutoff     string
	// ReplayErr is what a replay answers with, which is how a conflict is asked for.
	ReplayErr error
	// Own is how many commits the branch holds that the base does not.
	Own    int
	Remote git.RemoteStanding
	// RemoteErr is what reading origin's copy of the branch answers with.
	RemoteErr error
	Changes   git.Changes
	// PushErr is what the push answers with, and SHA what it answers when it goes through.
	PushErr error
	SHA     string
	// Pushed is every push asked for, in order.
	Pushed   []git.PushParams
	Sequence git.Sequence
	// Onto is the commit a rebase in progress is replaying onto.
	Onto string
	// AbortErr is what the abort answers with.
	AbortErr error
	// Calls is what was asked of git, in the order it was asked.
	Calls []string
	// BeforeReadTree runs ahead of every read of the tree.
	BeforeReadTree func()
	// Holds is every commit HEAD's history holds.
	Holds []string
	// Tips is the commit origin's copy of each branch names.
	Tips map[string]string
	// Ancestors is every commit pair, ancestor then descendant, the history holds.
	Ancestors [][2]string
}

// ReadTreeState answers State.
func (f *Mover) ReadTreeState(context.Context, string) (git.TreeState, error) {
	if f.BeforeReadTree != nil {
		f.BeforeReadTree()
	}

	return f.State, nil
}

// FetchBase answers origin/main.
func (f *Mover) FetchBase(_ context.Context, _, _ string) (ref, sha string, err error) {
	return "origin/main", "basesha", nil
}

// CountCommitsToReplay answers Commits.
func (f *Mover) CountCommitsToReplay(context.Context, string, string) (int, error) {
	return f.Commits, nil
}

// PredictRebase answers Prediction.
func (f *Mover) PredictRebase(context.Context, string, string) (git.Prediction, error) {
	return f.Prediction, nil
}

// Park records the ref it parks.
func (f *Mover) Park(_ context.Context, _, ref string) (string, error) {
	f.Parked = append(f.Parked, ref)
	f.Calls = append(f.Calls, "park")

	return "parkedsha", nil
}

// ReleaseRefsUnder does nothing.
func (f *Mover) ReleaseRefsUnder(context.Context, string, string, string) error {
	return nil
}

// Rebase records the call and answers ReplayErr.
func (f *Mover) Rebase(context.Context, string, string) error {
	f.Calls = append(f.Calls, "rebase")

	return f.ReplayErr
}

// RebaseOnto records the cutoff and answers ReplayErr.
func (f *Mover) RebaseOnto(_ context.Context, _, _, cutoff string) error {
	f.Cutoff = cutoff
	f.Calls = append(f.Calls, "rebaseOnto")

	return f.ReplayErr
}

// CountAgainstRemote answers Remote or RemoteErr.
func (f *Mover) CountAgainstRemote(context.Context, string, string) (*git.RemoteStanding, error) {
	if f.RemoteErr != nil {
		return nil, f.RemoteErr
	}

	return &f.Remote, nil
}

// ResolveBase answers origin and the base.
func (f *Mover) ResolveBase(_ context.Context, _, base string) (string, error) {
	return "origin/" + base, nil
}

// CountAheadBehind answers Own ahead and nothing behind.
func (f *Mover) CountAheadBehind(context.Context, string, string) (ahead, behind int, err error) {
	return f.Own, 0, nil
}

// PeekChanges answers Changes.
func (f *Mover) PeekChanges(context.Context, string) (git.Changes, error) {
	return f.Changes, nil
}

// Push records the push and answers SHA or PushErr.
func (f *Mover) Push(_ context.Context, params git.PushParams) (string, error) {
	f.Pushed = append(f.Pushed, params)
	if f.PushErr != nil {
		return "", f.PushErr
	}

	return f.SHA, nil
}

// ReadSequence answers Sequence.
func (f *Mover) ReadSequence(context.Context, string) (git.Sequence, error) {
	return f.Sequence, nil
}

// ReadRebaseOnto answers Onto.
func (f *Mover) ReadRebaseOnto(context.Context, string) (string, error) {
	return f.Onto, nil
}

// AbortSequence records the abort and, unless AbortErr says otherwise, leaves nothing in progress.
func (f *Mover) AbortSequence(_ context.Context, _ string, sequence git.Sequence) error {
	f.Calls = append(f.Calls, "abort "+string(sequence))
	if f.AbortErr != nil {
		return f.AbortErr
	}
	f.Sequence = git.SequenceNone

	return nil
}

// RefreshRemoteBranch records the refresh.
func (f *Mover) RefreshRemoteBranch(context.Context, string, string) error {
	f.Calls = append(f.Calls, "refresh")

	return nil
}

// HoldsCommit answers whether Holds names sha.
func (f *Mover) HoldsCommit(_ context.Context, _, sha string) (bool, error) {
	return slices.Contains(f.Holds, sha), nil
}

// FetchBranchTip answers the branch's entry in Tips, or ErrBranchNotOnOrigin when it has none.
func (f *Mover) FetchBranchTip(_ context.Context, _, branch string) (string, error) {
	f.Calls = append(f.Calls, "fetch "+branch)

	tip, ok := f.Tips[branch]
	if !ok {
		return "", git.ErrBranchNotOnOrigin
	}

	return tip, nil
}

// IsAncestor answers whether Ancestors lists the pair.
func (f *Mover) IsAncestor(_ context.Context, params git.IsAncestorParams) (bool, error) {
	return slices.Contains(f.Ancestors, [2]string{params.MaybeAncestor, params.Of}), nil
}

// ResolveCommit answers the revision as its own full name.
func (f *Mover) ResolveCommit(_ context.Context, _, rev string) (string, error) {
	return rev, nil
}
