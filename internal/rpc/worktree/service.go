// Package worktree serves the directories weave cut for its agents.
package worktree

import (
	"context"

	"weave/internal/fail"
	"weave/internal/git"
	"weave/internal/rpcerr"
	"weave/internal/worktrees"
	"weave/lib/dates"
	"weave/lib/xslice"
	worktreev1 "weave/proto/gen/worktree/v1"

	"google.golang.org/protobuf/types/known/emptypb"
)

type lister interface {
	List(ctx context.Context) (worktrees.Listing, error)
}

type inspector interface {
	Inspect(ctx context.Context, dir string, fetch bool) (worktrees.Inspection, error)
}

type remover interface {
	Remove(ctx context.Context, dirs []string) (worktrees.Removal, error)
}

type deleter interface {
	DeleteWorktrees(ctx context.Context, dirs []string) error
}

type WorktreeService struct {
	lister    lister
	inspector inspector
	remover   remover
	deleter   deleter
}

type NewWorktreeServiceParams struct {
	Lister    lister
	Inspector inspector
	Remover   remover
	Deleter   deleter
}

// NewWorktreeService constructs the handler over the worktree readers, the remover and the
// queue the bulk delete goes to.
func NewWorktreeService(params NewWorktreeServiceParams) *WorktreeService {
	return &WorktreeService{
		lister:    params.Lister,
		inspector: params.Inspector,
		remover:   params.Remover,
		deleter:   params.Deleter,
	}
}

// ListWorktrees is every directory under the worktree directories.
func (s *WorktreeService) ListWorktrees(ctx context.Context, _ *emptypb.Empty) (*worktreev1.ListWorktreesResponse, *fail.Error) {
	listing, err := s.lister.List(ctx)
	if err != nil {
		return nil, rpcerr.From(err, "failed to list worktrees")
	}

	return &worktreev1.ListWorktreesResponse{
		Worktrees:      xslice.Map(listing.Worktrees, renderWorktree),
		Dirs:           listing.Dirs,
		UnreadableDirs: listing.UnreadableDirs,
	}, nil
}

// GetWorktree reads one directory through git.
func (s *WorktreeService) GetWorktree(ctx context.Context, req *worktreev1.GetWorktreeRequest) (*worktreev1.GetWorktreeResponse, *fail.Error) {
	inspection, err := s.inspector.Inspect(ctx, req.GetDir(), req.GetFetch())
	if err != nil {
		return nil, rpcerr.From(err, "failed to read the worktree")
	}

	return &worktreev1.GetWorktreeResponse{
		Worktree:  renderWorktree(inspection.Worktree),
		SizeBytes: inspection.Bytes,
		Entries: xslice.Map(inspection.Entries, func(entry worktrees.Entry) *worktreev1.Entry {
			return &worktreev1.Entry{Path: entry.Path, SizeBytes: entry.Bytes, Dir: entry.Dir}
		}),
		Modified:           inspection.Changes.Modified,
		Untracked:          inspection.Changes.Untracked,
		BaseRef:            inspection.BaseRef,
		Ahead:              int32(inspection.Ahead),
		Behind:             int32(inspection.Behind),
		LandedCommits:      int32(inspection.Landed),
		SequenceInProgress: inspection.SequenceInProgress,
		KeepsBranch:        inspection.KeepsBranch,
		Commits: xslice.Map(inspection.Commits, func(commit git.Commit) *worktreev1.Commit {
			return &worktreev1.Commit{Sha: commit.SHA, Subject: commit.Subject, CommittedAt: dates.Stamp(commit.CommittedAt)}
		}),
	}, nil
}

// RemoveUnownedWorktrees takes away the directories no agent answers for.
func (s *WorktreeService) RemoveUnownedWorktrees(ctx context.Context, req *worktreev1.RemoveUnownedWorktreesRequest) (*worktreev1.RemoveUnownedWorktreesResponse, *fail.Error) {
	removal, err := s.remover.Remove(ctx, req.GetDirs())
	if err != nil {
		return nil, rpcerr.From(err, "failed to remove worktrees")
	}

	return &worktreev1.RemoveUnownedWorktreesResponse{
		Removed: removal.Removed,
		Refused: xslice.Map(removal.Refused, func(refusal worktrees.Refusal) *worktreev1.Refusal {
			return &worktreev1.Refusal{Dir: refusal.Dir, Reason: refusal.Reason}
		}),
	}, nil
}

// DeleteWorktrees queues the delete of the directories named as one background job.
func (s *WorktreeService) DeleteWorktrees(ctx context.Context, req *worktreev1.DeleteWorktreesRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.deleter.DeleteWorktrees(ctx, req.GetDirs()); err != nil {
		return nil, rpcerr.From(err, "failed to queue the delete")
	}

	return &emptypb.Empty{}, nil
}

func renderWorktree(worktree worktrees.Worktree) *worktreev1.Worktree {
	rendered := &worktreev1.Worktree{
		Dir:          worktree.Dir,
		RepoDir:      worktree.RepoDir,
		Branch:       worktree.Branch,
		Readable:     worktree.Readable,
		Occupants:    xslice.Map(worktree.Occupants, renderAgent),
		LastActiveAt: dates.Stamp(worktree.LastActiveAt),
	}
	if worktree.Agent != nil {
		rendered.Agent = renderAgent(*worktree.Agent)
	}
	if size := worktree.Size; size != nil {
		rendered.MeasuredAt = dates.StampPtr(&size.MeasuredAt)
		rendered.SizeError = size.Error
		if size.Error == "" {
			rendered.SizeBytes = &size.Bytes
		}
	}

	return rendered
}

func renderAgent(agent worktrees.Agent) *worktreev1.WorktreeAgent {
	return &worktreev1.WorktreeAgent{
		Id:         agent.ID,
		Name:       agent.Name,
		State:      string(agent.State),
		ArchivedAt: dates.StampPtr(agent.ArchivedAt),
		Adopted:    agent.Adopted,
	}
}
