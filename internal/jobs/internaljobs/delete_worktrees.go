package internaljobs

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"weave/internal/logging"
	"weave/internal/worktrees"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// deleteWorktreesBatch is how many agents one delete covers.
const deleteWorktreesBatch = 25

type DeleteWorktreesArgs struct {
	Dirs []string `json:"dirs"`
}

// Kind is the River job kind.
func (DeleteWorktreesArgs) Kind() string { return "delete_worktrees" }

// InsertOpts keeps one job per set of directories in flight, using River's default states
// without completed so a finished delete does not swallow a later press naming the same rows.
func (DeleteWorktreesArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		UniqueOpts: river.UniqueOpts{
			ByArgs: true,
			ByState: []rivertype.JobState{
				rivertype.JobStateAvailable,
				rivertype.JobStatePending,
				rivertype.JobStateRetryable,
				rivertype.JobStateRunning,
				rivertype.JobStateScheduled,
			},
		},
	}
}

type WorktreeLister interface {
	List(ctx context.Context) (worktrees.Listing, error)
}

type WorktreeRemover interface {
	Remove(ctx context.Context, dirs []string) (worktrees.Removal, error)
}

type DeleteWorktreesWorker struct {
	river.WorkerDefaults[DeleteWorktreesArgs]
	worktrees WorktreeLister
	deleter   AgentDeleter
	remover   WorktreeRemover
	logger    *slog.Logger
}

type NewDeleteWorktreesWorkerParams struct {
	Worktrees WorktreeLister
	Deleter   AgentDeleter
	Remover   WorktreeRemover
	Logger    *slog.Logger
}

// NewDeleteWorktreesWorker builds the worker over the listing that says how each directory
// goes and the two services that take it away.
func NewDeleteWorktreesWorker(params NewDeleteWorktreesWorkerParams) *DeleteWorktreesWorker {
	return &DeleteWorktreesWorker{
		worktrees: params.Worktrees,
		deleter:   params.Deleter,
		remover:   params.Remover,
		logger:    cmp.Or(params.Logger, slog.Default()),
	}
}

// Work sends each directory the listing still holds down the path its row decides, and
// cancels rather than retries when a refusal is all that went wrong.
func (w *DeleteWorktreesWorker) Work(ctx context.Context, job *river.Job[DeleteWorktreesArgs]) error {
	listing, err := w.worktrees.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list the worktrees: %w", err)
	}

	var ids, dirs []string
	var refused []worktrees.Refusal
	for _, dir := range job.Args.Dirs {
		at := slices.IndexFunc(listing.Worktrees, func(worktree worktrees.Worktree) bool { return worktree.Dir == dir })
		if at < 0 {
			continue
		}

		worktree := listing.Worktrees[at]
		switch {
		case worktree.Agent == nil || worktree.IsStuck():
			dirs = append(dirs, dir)
		case worktree.Agent.ArchivedAt != nil:
			ids = append(ids, worktree.Agent.ID)
		default:
			refused = append(refused, worktrees.Refusal{Dir: dir, Reason: fmt.Sprintf("agent %s is live; archive it first", worktree.Agent.Name)})
		}
	}

	var failed []error
	for batch := range slices.Chunk(ids, deleteWorktreesBatch) {
		if err := w.deleter.DeleteAgents(ctx, batch); err != nil {
			failed = append(failed, fmt.Errorf("failed to delete %d agents: %w", len(batch), err))
		}
	}

	if len(dirs) > 0 {
		removal, err := w.remover.Remove(ctx, dirs)
		if err != nil {
			failed = append(failed, fmt.Errorf("failed to remove %d directories: %w", len(dirs), err))
		}
		refused = append(refused, removal.Refused...)
	}

	left := make([]error, 0, len(refused))
	for _, refusal := range refused {
		err := fmt.Errorf("%s was left alone: %s", refusal.Dir, refusal.Reason)
		w.logger.LogAttrs(ctx, slog.LevelWarn, "left a worktree alone",
			append([]slog.Attr{slog.String(logging.FilePath, refusal.Dir)}, logging.Err(err)...)...)
		left = append(left, err)
	}
	if len(failed) > 0 {
		return errors.Join(slices.Concat(failed, left)...)
	}
	if len(left) > 0 {
		return river.JobCancel(errors.Join(left...))
	}

	return nil
}
