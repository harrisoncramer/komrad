// Package agentgit runs git on the engine for a panel somewhere else, in the directories live
// agents work in.
package agentgit

import (
	"context"

	"komrad/internal/agentdirs"
	"komrad/internal/git"
	"komrad/internal/position"
)

type gitReader interface {
	GetRoot(ctx context.Context, dir string) (string, error)
	ReadFiles(ctx context.Context, dir string, revisions []git.FileRevision) (map[git.FileRevision][]byte, error)
	CountChangedFiles(ctx context.Context, params git.CountChangedFilesParams) (int, error)
	Diff(ctx context.Context, params git.DiffParams) ([]byte, error)
	MapRanges(ctx context.Context, params git.MapRangesParams) ([]position.Mapping, error)
	Replay(ctx context.Context, params git.ReplayParams) (*git.Replayed, error)
	IsAncestor(ctx context.Context, dir, maybeAncestor, of string) (bool, error)
}

// NewGitServiceParams is what the service is built over.
type NewGitServiceParams struct {
	Agents agentdirs.DirLister
	Git    gitReader
}

// GitService runs git in the directories agents work in, and nowhere else.
type GitService struct {
	agents agentdirs.DirLister
	git    gitReader
}

// NewGitService builds the service.
func NewGitService(params NewGitServiceParams) *GitService {
	return &GitService{agents: params.Agents, git: params.Git}
}

// GetRoot is the top of the working tree dir belongs to.
func (s *GitService) GetRoot(ctx context.Context, dir string) (string, error) {
	resolved, err := agentdirs.Resolve(ctx, s.agents, dir)
	if err != nil {
		return "", err
	}

	return s.git.GetRoot(ctx, resolved)
}

// ReadFiles is every named file as its commit holds it, with a file the commit does not hold
// left out.
func (s *GitService) ReadFiles(ctx context.Context, dir string, revisions []git.FileRevision) (map[git.FileRevision][]byte, error) {
	resolved, err := agentdirs.Resolve(ctx, s.agents, dir)
	if err != nil {
		return nil, err
	}

	return s.git.ReadFiles(ctx, resolved, revisions)
}

// CountChangedFiles is how many files differ between two commits.
func (s *GitService) CountChangedFiles(ctx context.Context, params git.CountChangedFilesParams) (int, error) {
	resolved, err := agentdirs.Resolve(ctx, s.agents, params.Dir)
	if err != nil {
		return 0, err
	}

	params.Dir = resolved

	return s.git.CountChangedFiles(ctx, params)
}

// Diff is the patch between two commits.
func (s *GitService) Diff(ctx context.Context, params git.DiffParams) ([]byte, error) {
	resolved, err := agentdirs.Resolve(ctx, s.agents, params.Dir)
	if err != nil {
		return nil, err
	}

	params.Dir = resolved

	return s.git.Diff(ctx, params)
}

// MapRanges is what the later commit did to each file.
func (s *GitService) MapRanges(ctx context.Context, params git.MapRangesParams) ([]position.Mapping, error) {
	resolved, err := agentdirs.Resolve(ctx, s.agents, params.Dir)
	if err != nil {
		return nil, err
	}

	params.Dir = resolved

	return s.git.MapRanges(ctx, params)
}

// Replay carries a snapshot's own change onto another base in memory.
func (s *GitService) Replay(ctx context.Context, params git.ReplayParams) (*git.Replayed, error) {
	resolved, err := agentdirs.Resolve(ctx, s.agents, params.Dir)
	if err != nil {
		return nil, err
	}

	params.Dir = resolved

	return s.git.Replay(ctx, params)
}

// IsAncestor reports whether one commit is reachable from another.
func (s *GitService) IsAncestor(ctx context.Context, dir, maybeAncestor, of string) (bool, error) {
	resolved, err := agentdirs.Resolve(ctx, s.agents, dir)
	if err != nil {
		return false, err
	}

	return s.git.IsAncestor(ctx, resolved, maybeAncestor, of)
}
