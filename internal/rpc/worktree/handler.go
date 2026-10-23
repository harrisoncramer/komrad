// Package worktree serves the directories komrad cut for its agents.
package worktree

import (
	"context"

	"komrad/internal/fail"
	"komrad/internal/git"
	"komrad/internal/rpcerr"
	"komrad/internal/worktrees"
	"komrad/lib/dates"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	worktreev1 "komrad/proto/gen/worktree/v1"

	"google.golang.org/protobuf/types/known/emptypb"
)

type lister interface {
	ListUnclaimed(ctx context.Context) (*worktrees.Listing, error)
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

type NewHandlerParams struct {
	Lister    lister
	Inspector inspector
	Remover   remover
	Deleter   deleter
}

type Handler struct {
	lister    lister
	inspector inspector
	remover   remover
	deleter   deleter
}

// NewHandler constructs the handler over the worktree readers, the remover and the queue
// the bulk delete goes to.
func NewHandler(params NewHandlerParams) *Handler {
	return &Handler{
		lister:    params.Lister,
		inspector: params.Inspector,
		remover:   params.Remover,
		deleter:   params.Deleter,
	}
}

// ListWorktrees is every directory under the worktree directories that no delete has claimed.
func (s *Handler) ListWorktrees(ctx context.Context, _ *emptypb.Empty) (*worktreev1.ListWorktreesResponse, *fail.Error) {
	listing, err := s.lister.ListUnclaimed(ctx)
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
		UnpulledLanded:     inspection.RemoteLanded,
		Commits: xslice.Map(inspection.Commits, func(commit git.Commit) *worktreev1.Commit {
			return &worktreev1.Commit{
				Sha:         commit.SHA,
				Subject:     commit.Subject,
				CommittedAt: dates.Stamp(commit.CommittedAt),
			}
		}),
	}, nil
}

// RemoveUnownedWorktrees takes away the directories no agent or task answers for.
func (s *Handler) RemoveUnownedWorktrees(ctx context.Context, req *worktreev1.RemoveUnownedWorktreesRequest) (*worktreev1.RemoveUnownedWorktreesResponse, *fail.Error) {
	removal, err := s.remover.Remove(ctx, req.GetDirs())
	if err != nil {
		return nil, rpcerr.From(err, "failed to remove worktrees")
	}

	return &worktreev1.RemoveUnownedWorktreesResponse{
		Removed: removal.Removed,
		Refused: xslice.Map(removal.Refused, renderRefusal),
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
	if worktree.Task != nil {
		rendered.Task = renderTask(*worktree.Task)
	}
	if size := worktree.Size; size != nil {
		rendered.MeasuredAt = dates.StampPtr(&size.MeasuredAt)
		rendered.SizeError = size.Error
		if size.Error == "" {
			rendered.SizeBytes = &size.Bytes
			rendered.ShareBytes = &size.Share
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
	case worktrees.FateDeleting:
		return worktreev1.WorktreeFate_WORKTREE_FATE_DELETING
	}

	return worktreev1.WorktreeFate_WORKTREE_FATE_UNSPECIFIED
}

func renderRefusal(refusal worktrees.Refusal) *worktreev1.Refusal {
	rendered := &worktreev1.Refusal{
		Dir:       refusal.Dir,
		Reason:    renderRefusalReason(refusal.Reason),
		TaskSlug:  refusal.TaskSlug,
		AgentName: refusal.AgentName,
		NestedDir: refusal.NestedDir,
	}
	if refusal.Err != nil {
		rendered.Error = refusal.Err.Error()
	}

	return rendered
}

func renderRefusalReason(reason worktrees.RefusalReason) worktreev1.RefusalReason {
	switch reason {
	case worktrees.RefusedOutside:
		return worktreev1.RefusalReason_REFUSAL_REASON_OUTSIDE
	case worktrees.RefusedUnlisted:
		return worktreev1.RefusalReason_REFUSAL_REASON_UNLISTED
	case worktrees.RefusedTaskHolds:
		return worktreev1.RefusalReason_REFUSAL_REASON_TASK_HOLDS
	case worktrees.RefusedTaskLive:
		return worktreev1.RefusalReason_REFUSAL_REASON_TASK_LIVE
	case worktrees.RefusedTaskDeleting:
		return worktreev1.RefusalReason_REFUSAL_REASON_TASK_DELETING
	case worktrees.RefusedAgentInside:
		return worktreev1.RefusalReason_REFUSAL_REASON_AGENT_INSIDE
	case worktrees.RefusedAgentBeneath:
		return worktreev1.RefusalReason_REFUSAL_REASON_AGENT_BENEATH
	case worktrees.RefusedNestsWorktree:
		return worktreev1.RefusalReason_REFUSAL_REASON_NESTS_WORKTREE
	case worktrees.RefusedUnresolved:
		return worktreev1.RefusalReason_REFUSAL_REASON_UNRESOLVED
	case worktrees.RefusedListFailed:
		return worktreev1.RefusalReason_REFUSAL_REASON_LIST_FAILED
	case worktrees.RefusedUnlockFailed:
		return worktreev1.RefusalReason_REFUSAL_REASON_UNLOCK_FAILED
	case worktrees.RefusedRemoveFailed:
		return worktreev1.RefusalReason_REFUSAL_REASON_REMOVE_FAILED
	}

	return worktreev1.RefusalReason_REFUSAL_REASON_UNSPECIFIED
}

func renderAgent(agent worktrees.Agent) *worktreev1.WorktreeAgent {
	return &worktreev1.WorktreeAgent{
		Id:         agent.ID.String(),
		Name:       agent.Name,
		State:      agent.State,
		ArchivedAt: dates.StampPtr(agent.ArchivedAt),
	}
}

func renderTask(task worktrees.Task) *worktreev1.WorktreeTask {
	return &worktreev1.WorktreeTask{
		Id:         task.ID.String(),
		Slug:       task.Slug,
		ArchivedAt: dates.StampPtr(task.ArchivedAt),
		Adopted:    task.Adopted,
	}
}

func countLines(stat *git.DiffStat) (*int32, *int32) {
	if stat == nil {
		return nil, nil
	}

	return xptr.Ptr(int32(stat.Added)), xptr.Ptr(int32(stat.Removed))
}

func describeBaseErr(err error) string {
	if err == nil {
		return ""
	}

	return err.Error()
}
