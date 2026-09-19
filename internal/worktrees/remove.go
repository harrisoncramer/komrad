package worktrees

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"

	"weave/internal/logging"
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
	// FreedBytes is what the removed directories held by their last count.
	FreedBytes int64
}

type pruner interface {
	PruneWorktrees(ctx context.Context, repo string) error
}

type sizeDeleter interface {
	Delete(ctx context.Context, dirs []string) error
}

// Remover takes away the directories no agent answers for.
type Remover struct {
	lister *Lister
	git    pruner
	sizes  sizeDeleter
}

type NewRemoverParams struct {
	Lister *Lister
	Git    pruner
	Sizes  sizeDeleter
}

// NewRemover builds a remover over the lister, git and the size store.
func NewRemover(params NewRemoverParams) *Remover {
	return &Remover{lister: params.Lister, git: params.Git, sizes: params.Sizes}
}

// Remove takes away each directory nothing answers for, refusing the rest by name.
func (r *Remover) Remove(ctx context.Context, dirs []string) (Removal, error) {
	listing, err := r.lister.List(ctx)
	if err != nil {
		return Removal{}, err
	}

	removal := Removal{}
	for _, dir := range dirs {
		dir = filepath.Clean(dir)
		if reason := r.refuse(listing, dir); reason != "" {
			removal.Refused = append(removal.Refused, Refusal{Dir: dir, Reason: reason})

			continue
		}

		at := slices.IndexFunc(listing.Worktrees, func(w Worktree) bool { return w.Dir == dir })
		worktree := listing.Worktrees[at]

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

		if worktree.Size != nil {
			removal.FreedBytes += worktree.Size.Bytes
		}
		removal.Removed = append(removal.Removed, dir)
	}

	if err := r.sizes.Delete(ctx, removal.Removed); err != nil {
		return Removal{}, err
	}

	return removal, nil
}

// refuse is why a directory is not this remover's to take, empty when it is.
func (r *Remover) refuse(listing Listing, dir string) string {
	inside, err := contains(listing.Dirs, dir)
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
	if worktree.Agent != nil && !worktree.Agent.FailedToCleanup {
		return fmt.Sprintf("agent %s answers for it; delete the agent instead", worktree.Agent.Name)
	}
	if len(worktree.Children) > 0 {
		return fmt.Sprintf("agent %s is working in it; delete the agent instead", worktree.Children[0].Name)
	}

	return ""
}
