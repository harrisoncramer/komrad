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
	// Failed says a step broke rather than the directory being one the remover will not take.
	Failed bool
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

type NewRemoverParams struct {
	Lister *Lister
	Agents agentDirLister
	Git    registrar
}

// Remover takes away the directories no agent answers for.
type Remover struct {
	lister *Lister
	agents agentDirLister
	git    registrar
}

// NewRemover builds a remover over the lister, the agent rows and git.
func NewRemover(params NewRemoverParams) *Remover {
	return &Remover{lister: params.Lister, agents: params.Agents, git: params.Git}
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

		if err := os.RemoveAll(dir); err != nil {
			removal.Refused = append(removal.Refused, Refusal{
				Dir:    dir,
				Reason: fmt.Sprintf("could not remove it: %v", err),
				Failed: true,
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
		return Refusal{Dir: dir, Reason: err.Error(), Failed: true}, true
	}
	if !inside {
		return Refusal{Dir: dir, Reason: "it is not under the worktree directories"}, true
	}

	at := slices.IndexFunc(listing.Worktrees, func(w Worktree) bool { return w.Dir == dir })
	if at < 0 {
		return Refusal{Dir: dir, Reason: "the listing does not hold it"}, true
	}

	worktree := listing.Worktrees[at]
	if worktree.Fate() != FateUnowned {
		return Refusal{Dir: dir, Reason: fmt.Sprintf("agent %s answers for it; delete the agent instead", worktree.Agent.Name)}, true
	}
	if len(worktree.Occupants) > 0 {
		return Refusal{Dir: dir, Reason: fmt.Sprintf("agent %s is working in it; delete the agent instead", worktree.Occupants[0].Name)}, true
	}
	if under := slices.IndexFunc(rows, func(row agentstore.AgentDir) bool { return isBeneath(dir, row.Cwd) }); under >= 0 {
		return Refusal{Dir: dir, Reason: fmt.Sprintf("agent %s is working under it; delete the agent instead", rows[under].Name)}, true
	}
	if nested := slices.IndexFunc(listing.Worktrees, func(w Worktree) bool { return isBeneath(dir, w.Dir) }); nested >= 0 {
		return Refusal{Dir: dir, Reason: fmt.Sprintf("it holds the worktree %s; remove that first", listing.Worktrees[nested].Dir)}, true
	}

	return Refusal{}, false
}

// refuseRegistered is what keeps a readable directory from being taken by what its repository
// has registered, false when nothing registered sits inside it.
func (r *Remover) refuseRegistered(ctx context.Context, worktree Worktree) (Refusal, bool) {
	if !worktree.Readable {
		return Refusal{}, false
	}

	registered, err := r.git.ListWorktrees(ctx, worktree.RepoDir)
	if err != nil {
		return Refusal{
			Dir:    worktree.Dir,
			Reason: fmt.Sprintf("could not list the worktrees of its repository: %v", err),
			Failed: true,
		}, true
	}
	if nested := slices.IndexFunc(registered, func(w git.Worktree) bool { return isBeneath(worktree.Dir, w.Dir) }); nested >= 0 {
		return Refusal{
			Dir:    worktree.Dir,
			Reason: fmt.Sprintf("it holds the worktree %s; remove that first", registered[nested].Dir),
		}, true
	}

	return Refusal{}, false
}

// isBeneath reports whether a path is somewhere inside a directory.
func isBeneath(dir, path string) bool {
	return strings.HasPrefix(filepath.Clean(path), dir+string(filepath.Separator))
}
