package worktrees

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"weave/internal/git"
	"weave/internal/logging"
	agentstore "weave/internal/store/agent"
)

// Refusal is a directory a removal left alone, and why.
type Refusal struct {
	Dir    string
	Reason string
}

// Removal is what one call took and what it left.
type Removal struct {
	Removed []string
	Refused []Refusal
}

type registrar interface {
	ListWorktrees(ctx context.Context, repo string) ([]git.Worktree, error)
	PruneWorktrees(ctx context.Context, repo string) error
}

// Remover takes away the directories no agent answers for.
type Remover struct {
	lister *Lister
	git    registrar
}

type NewRemoverParams struct {
	Lister *Lister
	Git    registrar
}

// NewRemover builds a remover over the lister and git.
func NewRemover(params NewRemoverParams) *Remover {
	return &Remover{lister: params.Lister, git: params.Git}
}

// Remove takes away each directory nothing answers for, refusing the rest by name.
func (r *Remover) Remove(ctx context.Context, dirs []string) (Removal, error) {
	listing, err := r.lister.List(ctx)
	if err != nil {
		return Removal{}, err
	}

	rows, err := r.lister.agents.ListDirs(ctx)
	if err != nil {
		return Removal{}, err
	}

	removal := Removal{}
	for _, dir := range dirs {
		dir = filepath.Clean(dir)
		if reason := refuse(listing, rows, dir); reason != "" {
			removal.Refused = append(removal.Refused, Refusal{Dir: dir, Reason: reason})

			continue
		}

		at := slices.IndexFunc(listing.Worktrees, func(w Worktree) bool { return w.Dir == dir })
		worktree := listing.Worktrees[at]

		if reason := r.refuseRegistered(ctx, worktree); reason != "" {
			removal.Refused = append(removal.Refused, Refusal{Dir: dir, Reason: reason})

			continue
		}

		if err := os.RemoveAll(dir); err != nil {
			removal.Refused = append(removal.Refused, Refusal{Dir: dir, Reason: fmt.Sprintf("could not remove it: %v", err)})

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

// refuse is why a directory is not this remover's to take, empty when it is.
func refuse(listing Listing, rows []agentstore.AgentDir, dir string) string {
	inside, err := isUnder(listing.Dirs, dir)
	if err != nil {
		return err.Error()
	}
	if !inside {
		return "it is not under the worktree directories"
	}

	at := slices.IndexFunc(listing.Worktrees, func(w Worktree) bool { return w.Dir == dir })
	if at < 0 {
		return "the listing does not hold it"
	}

	worktree := listing.Worktrees[at]
	if worktree.Agent != nil && !isStuck(worktree) {
		return fmt.Sprintf("agent %s answers for it; delete the agent instead", worktree.Agent.Name)
	}
	if len(worktree.Occupants) > 0 {
		return fmt.Sprintf("agent %s is working in it; delete the agent instead", worktree.Occupants[0].Name)
	}
	if under := slices.IndexFunc(rows, func(row agentstore.AgentDir) bool { return isBeneath(dir, row.Cwd) }); under >= 0 {
		return fmt.Sprintf("agent %s is working under it; delete the agent instead", rows[under].Name)
	}
	if nested := slices.IndexFunc(listing.Worktrees, func(w Worktree) bool { return isBeneath(dir, w.Dir) }); nested >= 0 {
		return fmt.Sprintf("it holds the worktree %s; remove that first", listing.Worktrees[nested].Dir)
	}

	return ""
}

// refuseRegistered is why a readable directory is not to be taken by what its repository has
// registered, empty when nothing registered sits inside it.
func (r *Remover) refuseRegistered(ctx context.Context, worktree Worktree) string {
	if !worktree.Readable {
		return ""
	}

	registered, err := r.git.ListWorktrees(ctx, worktree.RepoDir)
	if err != nil {
		return fmt.Sprintf("could not list the worktrees of its repository: %v", err)
	}
	if nested := slices.IndexFunc(registered, func(w git.Worktree) bool { return isBeneath(worktree.Dir, w.Dir) }); nested >= 0 {
		return fmt.Sprintf("it holds the worktree %s; remove that first", registered[nested].Dir)
	}

	return ""
}

// isBeneath reports whether a path is somewhere inside a directory.
func isBeneath(dir, path string) bool {
	return strings.HasPrefix(filepath.Clean(path), dir+string(filepath.Separator))
}

// isStuck reports whether the row's own delete failed on a registration git cannot read.
func isStuck(worktree Worktree) bool {
	return worktree.Agent.FailedToCleanup && !worktree.Readable
}
