package ui

import (
	"context"

	"weave/internal/git"
	"weave/internal/position"
)

// GitClient answers the panel's git with git.
type GitClient struct{}

// Materialise checks a round out as a worktree of its own.
func (GitClient) Materialise(ctx context.Context, dir, sha, at string) error {
	return git.Materialise(ctx, dir, sha, at)
}

// WorktreeHead is the commit a checkout stands on.
func (GitClient) WorktreeHead(ctx context.Context, at string) (string, bool) {
	return git.WorktreeHead(ctx, at)
}

// IsDirty reports whether a checkout holds anything the commit it stands on does not.
func (GitClient) IsDirty(ctx context.Context, at string) (bool, error) {
	return git.IsDirty(ctx, at)
}

// Reap removes a checkout and the registration that names it.
func (GitClient) Reap(ctx context.Context, dir, at string) error {
	return git.Reap(ctx, dir, at)
}

// Edits are the runs of lines a checkout holds that its commit does not.
func (GitClient) Edits(ctx context.Context, dir string) ([]git.Edit, error) {
	return git.Edits(ctx, dir)
}

// ReadFileLines reads one file as a commit holds it.
func (GitClient) ReadFileLines(ctx context.Context, dir, sha, path string) ([]string, error) {
	return git.ReadFileLines(ctx, dir, sha, path)
}

// CountChangedFiles is how many files differ between two commits.
func (GitClient) CountChangedFiles(ctx context.Context, params git.CountChangedFilesParams) (int, error) {
	return git.CountChangedFiles(ctx, params)
}

// MapRanges follows line ranges from one commit to another.
func (GitClient) MapRanges(ctx context.Context, params git.MapRangesParams) ([]position.Mapping, error) {
	return git.MapRanges(ctx, params)
}

// ProbeTree is the tree a checkout would freeze to right now.
func (GitClient) ProbeTree(ctx context.Context, dir string) (string, error) {
	return git.ProbeTree(ctx, dir)
}

// GetTree is the tree a commit holds.
func (GitClient) GetTree(ctx context.Context, dir, sha string) (string, error) {
	return git.GetTree(ctx, dir, sha)
}
