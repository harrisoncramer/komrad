// Package gittest holds fakes of the git a test would otherwise run for real.
package gittest

import (
	"context"

	"weave/internal/fail"
	"weave/internal/git"
	"weave/internal/position"
)

// ErrNoGit is what the fake answers with.
var ErrNoGit = fail.Precondition("this test was given no git")

// NoRepo answers as a checkout with no repository behind it.
type NoRepo struct{}

// ReadFileLines refuses.
func (NoRepo) ReadFileLines(context.Context, string, string, string) ([]string, error) {
	return nil, ErrNoGit
}

// CountChangedFiles refuses.
func (NoRepo) CountChangedFiles(context.Context, git.CountChangedFilesParams) (int, error) {
	return 0, ErrNoGit
}

// MapRanges refuses.
func (NoRepo) MapRanges(context.Context, git.MapRangesParams) ([]position.Mapping, error) {
	return nil, ErrNoGit
}

// Replay refuses.
func (NoRepo) Replay(context.Context, git.ReplayParams) (*git.Replayed, error) {
	return nil, ErrNoGit
}

// IsAncestor refuses.
func (NoRepo) IsAncestor(context.Context, string, string, string) (bool, error) {
	return false, ErrNoGit
}

// GetRoot refuses.
func (NoRepo) GetRoot(context.Context, string) (string, error) {
	return "", ErrNoGit
}

// Diff refuses.
func (NoRepo) Diff(context.Context, git.DiffParams) ([]byte, error) {
	return nil, ErrNoGit
}

// ReadFiles refuses.
func (NoRepo) ReadFiles(context.Context, string, []git.FileRevision) (map[git.FileRevision][]byte, error) {
	return nil, ErrNoGit
}
