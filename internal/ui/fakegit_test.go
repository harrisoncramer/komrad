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

func (fakeGit) Materialise(context.Context, string, string, string) error { return errNoGit }

func (fakeGit) WorktreeHead(context.Context, string) (string, bool) { return "", false }

func (fakeGit) IsDirty(context.Context, string) (bool, error) { return false, errNoGit }

func (fakeGit) Reap(context.Context, string, string) error { return errNoGit }

func (fakeGit) Edits(context.Context, string) ([]git.Edit, error) { return nil, errNoGit }

func (fakeGit) ReadFileLines(context.Context, string, string, string) ([]string, error) {
	return nil, errNoGit
}

func (fakeGit) CountChangedFiles(context.Context, git.CountChangedFilesParams) (int, error) {
	return 0, errNoGit
}

func (fakeGit) MapRanges(context.Context, git.MapRangesParams) ([]position.Mapping, error) {
	return nil, errNoGit
}

func (fakeGit) ProbeTree(context.Context, string) (string, error) { return "", errNoGit }

func (fakeGit) GetTree(context.Context, string, string) (string, error) { return "", errNoGit }
