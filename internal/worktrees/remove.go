package worktrees

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"komrad/internal/git"
	"komrad/internal/logging"
	agentstore "komrad/internal/store/agent"
)

// RefusalReason is why a removal left a directory alone.
type RefusalReason int

const (
	// RefusedOutside is a path not under the worktree directories.
	RefusedOutside RefusalReason = iota
	// RefusedUnlisted is a path the listing does not hold.
	RefusedUnlisted
	// RefusedTaskHolds is a directory its task answers for.
	RefusedTaskHolds
	// RefusedTaskLive is a directory whose task is open.
	RefusedTaskLive
	// RefusedTaskDeleting is a directory whose task a delete has claimed.
	RefusedTaskDeleting
	// RefusedAgentInside is a directory an agent works in.
	RefusedAgentInside
	// RefusedAgentBeneath is a directory an agent works somewhere under.
	RefusedAgentBeneath
	// RefusedNestsWorktree is a directory holding another worktree.
	RefusedNestsWorktree
	// RefusedUnresolved is a path that could not be looked up.
	RefusedUnresolved
	// RefusedListFailed is a directory whose repository's worktrees git could not list.
	RefusedListFailed
	// RefusedUnlockFailed is a directory whose lock git could not lift.
	RefusedUnlockFailed
	// RefusedRemoveFailed is a directory the filesystem would not let go.
	RefusedRemoveFailed
)

// Failed reports whether a step broke rather than the directory being one the remover will not take.
func (r RefusalReason) Failed() bool {
	switch r {
	case RefusedUnresolved, RefusedListFailed, RefusedUnlockFailed, RefusedRemoveFailed:
		return true
	case RefusedOutside, RefusedUnlisted, RefusedTaskHolds, RefusedTaskLive, RefusedTaskDeleting,
		RefusedAgentInside, RefusedAgentBeneath, RefusedNestsWorktree:
	}

	return false
}

// Refusal is a directory a removal left alone, why, and what the reason names.
type Refusal struct {
	Dir    string
	Reason RefusalReason
	// TaskSlug is the task a task reason names.
	TaskSlug string
	// AgentName is the agent an agent reason names.
	AgentName string
	// NestedDir is the worktree inside the directory.
	NestedDir string
	// Err is the step that broke, set when the reason failed.
	Err error
}

// Removal is what one call took and what it left.
type Removal struct {
	Removed []string
	Refused []Refusal
}

type registrar interface {
	ListWorktrees(ctx context.Context, repo string) ([]git.Worktree, error)
	UnlockWorktree(ctx context.Context, repo, dir string) error
	PruneWorktrees(ctx context.Context, repo string) error
}

type NewRemoverParams struct {
	Lister *Lister
	Agents *agentstore.AgentStore
	Git    registrar
}

// Remover takes away the directories no agent or task answers for.
type Remover struct {
	lister *Lister
	agents *agentstore.AgentStore
	git    registrar
}

// NewRemover builds a remover over the lister, the agent rows and git.
func NewRemover(params NewRemoverParams) *Remover {
	return &Remover{
		lister: params.Lister,
		agents: params.Agents,
		git:    params.Git,
	}
}

// RefuseForTask is the refusal of a directory its task answers for.
func (w Worktree) RefuseForTask() Refusal {
	return Refusal{Dir: w.Dir, Reason: RefusedTaskHolds, TaskSlug: w.Task.Slug}
}

// Remove takes away each directory nothing answers for, refusing the rest by name.
func (r *Remover) Remove(ctx context.Context, dirs []string) (*Removal, error) {
	listing, err := r.lister.List(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := r.agents.ListDirs(ctx)
	if err != nil {
		return nil, err
	}

	removal := &Removal{}
	for _, dir := range dirs {
		dir = filepath.Clean(dir)
		if refusal, refused := refuse(listing, rows, dir); refused {
			removal.Refused = append(removal.Refused, refusal)

			continue
		}

		at := slices.IndexFunc(listing.Worktrees, func(w Worktree) bool { return w.Dir == dir })
		worktree := listing.Worktrees[at]

		if refusal, refused := r.refuseRegistered(ctx, worktree); refused {
			removal.Refused = append(removal.Refused, refusal)

			continue
		}

		if refusal, refused := r.refuseLocked(ctx, worktree); refused {
			removal.Refused = append(removal.Refused, refusal)

			continue
		}

		if err := os.RemoveAll(dir); err != nil {
			removal.Refused = append(removal.Refused, Refusal{
				Dir:    dir,
				Reason: RefusedRemoveFailed,
				Err:    err,
			})

			continue
		}

		if worktree.Readable {
			if err := r.git.PruneWorktrees(ctx, worktree.RepoDir); err != nil {
				logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "removed a worktree but could not prune its registration",
					append([]slog.Attr{slog.String(logging.FilePath, dir)}, logging.Err(err)...)...)
			}
		}

		removal.Removed = append(removal.Removed, dir)
	}

	return removal, nil
}

// refuse is what keeps a directory from being this remover's to take, false when it is.
func refuse(listing *Listing, rows []agentstore.AgentDir, dir string) (Refusal, bool) {
	inside, err := isUnder(listing.Dirs, dir)
	if err != nil {
		return Refusal{
			Dir:    dir,
			Reason: RefusedUnresolved,
			Err:    err,
		}, true
	}
	if !inside {
		return Refusal{Dir: dir, Reason: RefusedOutside}, true
	}

	at := slices.IndexFunc(listing.Worktrees, func(w Worktree) bool { return w.Dir == dir })
	if at < 0 {
		return Refusal{Dir: dir, Reason: RefusedUnlisted}, true
	}

	worktree := listing.Worktrees[at]
	switch worktree.Fate() {
	case FateLive, FateArchived:
		return worktree.RefuseForTask(), true
	case FateDeleting:
		return Refusal{Dir: dir, Reason: RefusedTaskDeleting, TaskSlug: worktree.Task.Slug}, true
	case FateUnowned:
	}
	if len(worktree.Occupants) > 0 {
		return Refusal{Dir: dir, Reason: RefusedAgentInside, AgentName: worktree.Occupants[0].Name}, true
	}
	if under := slices.IndexFunc(rows, func(row agentstore.AgentDir) bool { return isBeneath(dir, row.Cwd) }); under >= 0 {
		return Refusal{Dir: dir, Reason: RefusedAgentBeneath, AgentName: rows[under].Name}, true
	}
	if nested := slices.IndexFunc(listing.Worktrees, func(w Worktree) bool { return isBeneath(dir, w.Dir) }); nested >= 0 {
		return Refusal{Dir: dir, Reason: RefusedNestsWorktree, NestedDir: listing.Worktrees[nested].Dir}, true
	}

	return Refusal{}, false
}

// refuseRegistered stops komrad taking a readable directory its repository has registered,
// false when nothing registered sits inside it.
func (r *Remover) refuseRegistered(ctx context.Context, worktree Worktree) (Refusal, bool) {
	if !worktree.Readable {
		return Refusal{}, false
	}

	registered, err := r.git.ListWorktrees(ctx, worktree.RepoDir)
	if err != nil {
		return Refusal{
			Dir:    worktree.Dir,
			Reason: RefusedListFailed,
			Err:    err,
		}, true
	}
	if nested := slices.IndexFunc(registered, func(w git.Worktree) bool { return isBeneath(worktree.Dir, w.Dir) }); nested >= 0 {
		return Refusal{
			Dir:       worktree.Dir,
			Reason:    RefusedNestsWorktree,
			NestedDir: registered[nested].Dir,
		}, true
	}

	return Refusal{}, false
}

// refuseLocked lifts the lock on a directory's registration, refusing the directory when git cannot.
func (r *Remover) refuseLocked(ctx context.Context, worktree Worktree) (Refusal, bool) {
	if !worktree.Locked {
		return Refusal{}, false
	}

	if err := r.git.UnlockWorktree(ctx, worktree.RepoDir, worktree.Dir); err != nil {
		return Refusal{
			Dir:    worktree.Dir,
			Reason: RefusedUnlockFailed,
			Err:    err,
		}, true
	}

	return Refusal{}, false
}

// isBeneath reports whether a path is somewhere inside a directory.
func isBeneath(dir, path string) bool {
	return strings.HasPrefix(filepath.Clean(path), dir+string(filepath.Separator))
}
