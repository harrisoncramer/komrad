package gittest

import (
	"context"

	"komrad/internal/git"
)

// Pusher is the git a push drives, answering from its fields and recording each push.
type Pusher struct {
	State git.TreeState
	// Own is how many commits the branch holds that the base does not.
	Own     int
	Remote  git.RemoteStanding
	Changes git.Changes
	// PushErr is what the push answers with, and SHA what it answers when it goes through.
	PushErr error
	SHA     string
	// Pushed is every push asked for, in order.
	Pushed []git.PushParams
}

// ReadTreeState answers State.
func (f *Pusher) ReadTreeState(context.Context, string) (git.TreeState, error) {
	return f.State, nil
}

// CountAgainstRemote answers Remote.
func (f *Pusher) CountAgainstRemote(context.Context, string, string) (*git.RemoteStanding, error) {
	return &f.Remote, nil
}

// ResolveBase answers origin and the base.
func (f *Pusher) ResolveBase(_ context.Context, _, base string) (string, error) {
	return "origin/" + base, nil
}

// CountAheadBehind answers Own ahead and nothing behind.
func (f *Pusher) CountAheadBehind(context.Context, string, string) (ahead, behind int, err error) {
	return f.Own, 0, nil
}

// PeekChanges answers Changes.
func (f *Pusher) PeekChanges(context.Context, string) (git.Changes, error) {
	return f.Changes, nil
}

// Push records the push and answers SHA or PushErr.
func (f *Pusher) Push(_ context.Context, params git.PushParams) (string, error) {
	f.Pushed = append(f.Pushed, params)
	if f.PushErr != nil {
		return "", f.PushErr
	}

	return f.SHA, nil
}
