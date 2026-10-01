package gittest

import (
	"context"

	"komrad/internal/git"
)

// Aborter is the git an abort drives, answering from its fields and recording each call.
type Aborter struct {
	Sequence git.Sequence
	// Onto is the commit a rebase in progress is replaying onto.
	Onto   string
	Branch string
	// AbortErr is what the abort answers with.
	AbortErr error
	Parked   []string
	// Calls is what was asked of git, in the order it was asked.
	Calls []string
}

// ReadTreeState answers Branch.
func (f *Aborter) ReadTreeState(context.Context, string) (git.TreeState, error) {
	return git.TreeState{Branch: f.Branch}, nil
}

// ReadSequence answers Sequence.
func (f *Aborter) ReadSequence(context.Context, string) (git.Sequence, error) {
	return f.Sequence, nil
}

// ReadRebaseOnto answers Onto.
func (f *Aborter) ReadRebaseOnto(context.Context, string) (string, error) {
	return f.Onto, nil
}

// Park records the ref it parks.
func (f *Aborter) Park(_ context.Context, _, ref string) (string, error) {
	f.Parked = append(f.Parked, ref)
	f.Calls = append(f.Calls, "park")

	return "parkedsha", nil
}

// AbortSequence records the abort and, unless AbortErr says otherwise, leaves nothing in progress.
func (f *Aborter) AbortSequence(_ context.Context, _ string, sequence git.Sequence) error {
	f.Calls = append(f.Calls, "abort "+string(sequence))
	if f.AbortErr != nil {
		return f.AbortErr
	}
	f.Sequence = git.SequenceNone

	return nil
}
