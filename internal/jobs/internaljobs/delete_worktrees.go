package internaljobs

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"

	"komrad/internal/logging"
	"komrad/internal/worktrees"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

type DeleteWorktreesArgs struct {
	Dirs []string `json:"dirs"`
}

// Kind is the River job kind.
func (DeleteWorktreesArgs) Kind() string {
	return "delete_worktrees"
}

// InsertOpts keeps one job per set of directories in flight.
func (DeleteWorktreesArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: QueueDeleteWorktrees,
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
	List(ctx context.Context) (*worktrees.Listing, error)
}

type WorktreeRemover interface {
	Remove(ctx context.Context, dirs []string) (*worktrees.Removal, error)
}

type NewDeleteWorktreesWorkerParams struct {
	Worktrees WorktreeLister
	Remover   WorktreeRemover
	Logger    *slog.Logger
}

type DeleteWorktreesWorker struct {
	river.WorkerDefaults[DeleteWorktreesArgs]
	worktrees WorktreeLister
	remover   WorktreeRemover
	logger    *slog.Logger
}

// NewDeleteWorktreesWorker builds the worker over the listing that says how each directory
// goes and the two services that take it away.
func NewDeleteWorktreesWorker(params NewDeleteWorktreesWorkerParams) *DeleteWorktreesWorker {
	return &DeleteWorktreesWorker{
		worktrees: params.Worktrees,
		remover:   params.Remover,
		logger:    cmp.Or(params.Logger, slog.New(slog.DiscardHandler)),
	}
}

// Work sends each directory the listing still holds down the path its row decides, and
// cancels rather than retries when a refusal is all that stands.
func (w *DeleteWorktreesWorker) Work(ctx context.Context, job *river.Job[DeleteWorktreesArgs]) error {
	listing, err := w.worktrees.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list the worktrees: %w", err)
	}

	var tearing []string
	var dirs []string
	var refused []worktrees.Refusal
	for _, dir := range job.Args.Dirs {
		at := slices.IndexFunc(listing.Worktrees, func(worktree worktrees.Worktree) bool { return worktree.Dir == dir })
		if at < 0 {
			continue
		}

		worktree := listing.Worktrees[at]
		switch worktree.Fate() {
		case worktrees.FateUnowned:
			dirs = append(dirs, dir)
		case worktrees.FateDeleting:
			tearing = append(tearing, dir)
		case worktrees.FateLive:
			refused = append(refused, worktrees.Refusal{Dir: dir, Reason: worktrees.RefusedTaskLive, TaskSlug: worktree.Task.Slug})
		case worktrees.FateArchived:
			refused = append(refused, worktree.RefuseForTask())
		}
	}

	var failed []error
	for _, level := range deepestFirst(dirs) {
		removal, err := w.remover.Remove(ctx, level)
		if err != nil {
			failed = append(failed, fmt.Errorf("failed to remove %d directories: %w", len(level), err))

			continue
		}
		for _, refusal := range removal.Refused {
			switch {
			case refusal.Reason.Failed():
				failed = append(failed, fmt.Errorf("%s: %s", refusal.Dir, describeRefusal(refusal)))
			case hasAny(refusal.Dir, tearing):
				failed = append(failed, fmt.Errorf("%s: %s; a task's teardown is still running under it", refusal.Dir, describeRefusal(refusal)))
			default:
				refused = append(refused, refusal)
			}
		}
	}

	left := make([]error, 0, len(refused))
	for _, refusal := range refused {
		err := fmt.Errorf("%s was left alone: %s", refusal.Dir, describeRefusal(refusal))
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

// describeRefusal words why a removal left a directory alone.
func describeRefusal(refusal worktrees.Refusal) string {
	switch refusal.Reason {
	case worktrees.RefusedOutside:
		return "it is not under the worktree directories"
	case worktrees.RefusedUnlisted:
		return "the listing does not hold it"
	case worktrees.RefusedTaskHolds:
		return fmt.Sprintf("task %s holds it; delete the task instead", refusal.TaskSlug)
	case worktrees.RefusedTaskLive:
		return fmt.Sprintf("task %s is live; archive it first", refusal.TaskSlug)
	case worktrees.RefusedTaskDeleting:
		return fmt.Sprintf("task %s is being deleted, which takes it", refusal.TaskSlug)
	case worktrees.RefusedAgentInside:
		return fmt.Sprintf("agent %s is working in it; delete the agent instead", refusal.AgentName)
	case worktrees.RefusedAgentBeneath:
		return fmt.Sprintf("agent %s is working under it; delete the agent instead", refusal.AgentName)
	case worktrees.RefusedNestsWorktree:
		return fmt.Sprintf("it holds the worktree %s; remove that first", refusal.NestedDir)
	case worktrees.RefusedUnresolved:
		return fmt.Sprint(refusal.Err)
	case worktrees.RefusedListFailed:
		return fmt.Sprintf("could not list the worktrees of its repository: %v", refusal.Err)
	case worktrees.RefusedUnlockFailed:
		return fmt.Sprintf("could not unlock it: %v", refusal.Err)
	case worktrees.RefusedRemoveFailed:
		return fmt.Sprintf("could not remove it: %v", refusal.Err)
	}

	return ""
}

// deepestFirst groups the directories by depth with the deepest group first.
func deepestFirst(dirs []string) [][]string {
	depth := func(dir string) int { return strings.Count(dir, string(filepath.Separator)) }
	sorted := slices.SortedStableFunc(slices.Values(dirs), func(a, b string) int { return depth(b) - depth(a) })

	var groups [][]string
	for _, dir := range sorted {
		if last := len(groups) - 1; last >= 0 && depth(groups[last][0]) == depth(dir) {
			groups[last] = append(groups[last], dir)

			continue
		}
		groups = append(groups, []string{dir})
	}

	return groups
}

// hasAny reports whether any of the paths sits inside the directory.
func hasAny(dir string, paths []string) bool {
	return slices.ContainsFunc(paths, func(path string) bool {
		return strings.HasPrefix(filepath.Clean(path), dir+string(filepath.Separator))
	})
}
