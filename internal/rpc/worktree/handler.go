// Package worktree serves the directories komrad cut for its agents.
package worktree

import (
	"context"

	"komrad/internal/fail"
	"komrad/internal/git"
	"komrad/internal/rpc/render"
	"komrad/internal/rpcerr"
	"komrad/internal/worktrees"
	"komrad/lib/dates"
	"komrad/lib/xslice"
	worktreev1 "komrad/proto/gen/worktree/v1"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

type lister interface {
	List(ctx context.Context) (*worktrees.Listing, error)
}

type inspector interface {
	Inspect(ctx context.Context, params worktrees.InspectParams) (*worktrees.Inspection, error)
}

type remover interface {
	Remove(ctx context.Context, dirs []string) (*worktrees.Removal, error)
}

type deleter interface {
	DeleteWorktrees(ctx context.Context, dirs []string) error
}

type claimedDirLister interface {
	ListClaimedDirs(ctx context.Context) ([]string, error)
}

type NewHandlerParams struct {
	Lister    lister
	Inspector inspector
	Remover   remover
	Deleter   deleter
	Claimed   claimedDirLister
}

type Handler struct {
	lister    lister
	inspector inspector
	remover   remover
	deleter   deleter
	claimed   claimedDirLister
}

// NewHandler constructs the handler over the worktree readers, the remover, the queue
// the bulk delete goes to and the reader of what that queue still holds.
func NewHandler(params NewHandlerParams) *Handler {
	return &Handler{
		lister:    params.Lister,
		inspector: params.Inspector,
		remover:   params.Remover,
		deleter:   params.Deleter,
		claimed:   params.Claimed,
	}
}

// ListWorktrees is every directory under the worktree directories that no delete has claimed.
func (s *Handler) ListWorktrees(ctx context.Context, _ *emptypb.Empty) (*worktreev1.ListWorktreesResponse, *fail.Error) {
	listing, err := s.lister.List(ctx)
	if err != nil {
		return nil, rpcerr.From(err, "failed to list worktrees")
	}

	claimed, err := s.claimed.ListClaimedDirs(ctx)
	if err != nil {
		return nil, rpcerr.From(err, "failed to list worktrees")
	}

	return &worktreev1.ListWorktreesResponse{
		Worktrees:      xslice.Map(worktrees.Unclaimed(listing.Worktrees, claimed), renderWorktree),
		Dirs:           listing.Dirs,
		UnreadableDirs: listing.UnreadableDirs,
	}, nil
}

// GetWorktree reads one directory through git.
func (s *Handler) GetWorktree(ctx context.Context, req *worktreev1.GetWorktreeRequest) (*worktreev1.GetWorktreeResponse, *fail.Error) {
	inspection, err := s.inspector.Inspect(ctx, worktrees.InspectParams{Dir: req.GetDir(), Fetch: req.GetFetch()})
	if err != nil {
		return nil, rpcerr.From(err, "failed to read the worktree")
	}

	added, removed := countLines(inspection.DiffStat)

	return &worktreev1.GetWorktreeResponse{
		Worktree:  renderWorktree(inspection.Worktree),
		SizeBytes: inspection.Bytes,
		Entries: xslice.Map(inspection.Entries, func(entry worktrees.Entry) *worktreev1.Entry {
			return &worktreev1.Entry{
				Path:      entry.Path,
				SizeBytes: entry.Bytes,
				Dir:       entry.Dir,
			}
		}),
		Modified:           inspection.Changes.Modified,
		Untracked:          inspection.Changes.Untracked,
		BaseRef:            inspection.BaseRef,
		BaseError:          describeBaseErr(inspection.BaseErr),
		Ahead:              int32(inspection.Ahead),
		Behind:             int32(inspection.Behind),
		LandedCommits:      int32(inspection.Landed),
		SequenceInProgress: inspection.SequenceInProgress,
		KeepsBranch:        inspection.KeepsBranch,
		AddedLines:         added,
		RemovedLines:       removed,
		Pushed:             inspection.Remote.Pushed,
		Unpushed:           int32(inspection.Remote.Unpushed),
		Unpulled:           int32(inspection.Remote.Unpulled),
		Commits: xslice.Map(inspection.Commits, func(commit git.Commit) *worktreev1.Commit {
			return &worktreev1.Commit{
				Sha:         commit.SHA,
				Subject:     commit.Subject,
				CommittedAt: dates.Stamp(commit.CommittedAt),
			}
		}),
	}, nil
}

// RemoveUnownedWorktrees takes away the directories no agent answers for.
func (s *Handler) RemoveUnownedWorktrees(ctx context.Context, req *worktreev1.RemoveUnownedWorktreesRequest) (*worktreev1.RemoveUnownedWorktreesResponse, *fail.Error) {
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
func (s *Handler) DeleteWorktrees(ctx context.Context, req *worktreev1.DeleteWorktreesRequest) (*emptypb.Empty, *fail.Error) {
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
		Fate:         renderFate(worktree.Fate()),
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

func renderFate(fate worktrees.Fate) worktreev1.WorktreeFate {
	switch fate {
	case worktrees.FateUnowned:
		return worktreev1.WorktreeFate_WORKTREE_FATE_UNOWNED
	case worktrees.FateArchived:
		return worktreev1.WorktreeFate_WORKTREE_FATE_ARCHIVED
	case worktrees.FateLive:
		return worktreev1.WorktreeFate_WORKTREE_FATE_LIVE
	}

	return worktreev1.WorktreeFate_WORKTREE_FATE_UNSPECIFIED
}

func renderAgent(agent worktrees.Agent) *worktreev1.WorktreeAgent {
	return &worktreev1.WorktreeAgent{
		Id:         agent.ID,
		Name:       agent.Name,
		State:      render.AgentStateOf(agent.State),
		ArchivedAt: dates.StampPtr(agent.ArchivedAt),
		Adopted:    agent.Adopted,
	}
}

func countLines(stat *git.DiffStat) (*int32, *int32) {
	if stat == nil {
		return nil, nil
	}

	return proto.Int32(int32(stat.Added)), proto.Int32(int32(stat.Removed))
}

func describeBaseErr(err error) string {
	if err == nil {
		return ""
	}

	return err.Error()
}
