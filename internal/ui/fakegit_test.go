package ui

import (
	"context"
	"errors"

	"weave/internal/git"
	"weave/internal/position"
)

// errNoGit is what the fake answers with.
var errNoGit = errors.New("this test was given no git")

// fakeGit answers as a checkout with no repository behind it.
type fakeGit struct{}

func (fakeGit) ReadFileLines(context.Context, string, string, string) ([]string, error) {
	return nil, errNoGit
}

func (fakeGit) CountChangedFiles(context.Context, git.CountChangedFilesParams) (int, error) {
	return 0, errNoGit
}

func (fakeGit) MapRanges(context.Context, git.MapRangesParams) ([]position.Mapping, error) {
	return nil, errNoGit
}

func (fakeGit) Replay(context.Context, git.ReplayParams) (*git.Replayed, error) {
	return nil, errNoGit
}

func (fakeGit) IsAncestor(context.Context, string, string, string) (bool, error) {
	return false, errNoGit
}

func (fakeGit) GetRoot(context.Context, string) (string, error) { return "", errNoGit }

func (fakeGit) Diff(context.Context, git.DiffParams) ([]byte, error) { return nil, errNoGit }

func (fakeGit) ReadFiles(context.Context, string, []git.FileRevision) (map[git.FileRevision][]byte, error) {
	return nil, errNoGit
}
