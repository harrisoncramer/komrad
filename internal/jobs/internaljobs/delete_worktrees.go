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
	"komrad/lib/xslice"

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
		logger:    cmp.Or(params.Logger, slog.Default()),
	}
}

// Work sends each directory the listing still holds down the path its row decides, and
// cancels rather than retries when a refusal is all that stands.
func (w *DeleteWorktreesWorker) Work(ctx context.Context, job *river.Job[DeleteWorktreesArgs]) error {
	listing, err := w.worktrees.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list the worktrees: %w", err)
	}

	var owned []worktrees.Worktree
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
		case worktrees.FateArchived:
			owned = append(owned, worktree)
		case worktrees.FateLive:
			refused = append(refused, worktrees.Refusal{Dir: dir, Reason: fmt.Sprintf("agent %s is live; archive it first", worktree.Agent.Name)})
		}
	}

	var failed []error
	tearing := xslice.Map(owned, func(worktree worktrees.Worktree) string { return worktree.Dir })

	for _, level := range deepestFirst(dirs) {
		removal, err := w.remover.Remove(ctx, level)
		if err != nil {
			failed = append(failed, fmt.Errorf("failed to remove %d directories: %w", len(level), err))

			continue
		}
		for _, refusal := range removal.Refused {
			switch {
			case refusal.Failed:
				failed = append(failed, fmt.Errorf("%s: %s", refusal.Dir, refusal.Reason))
			case hasAny(refusal.Dir, tearing):
				failed = append(failed, fmt.Errorf("%s: %s; an agent's teardown is still running under it", refusal.Dir, refusal.Reason))
			default:
				refused = append(refused, refusal)
			}
		}
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
