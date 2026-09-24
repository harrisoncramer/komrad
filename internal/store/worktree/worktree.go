// Package worktree stores each worktree directory as the filesystem last showed it.
package worktree

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"weave/db/models"
	"weave/internal/store"
	"weave/lib/xslice"
)

// Worktree is one directory as the size loop last read it.
type Worktree struct {
	Dir      string
	RepoDir  string
	Branch   string
	Readable bool
	// ModifiedAt is when git last wrote the directory's .git file, else the directory's own
	// modification time, nil when neither could be read.
	ModifiedAt *time.Time
	// Size is the last measurement, nil until the first walk.
	Size *Size
}

// Size is what one directory took when it was last walked, or why the walk failed.
type Size struct {
	Dir        string
	Bytes      int64
	MeasuredAt time.Time
	// Error is why the last walk failed, empty when it did not.
	Error string
}

// WorktreeStore reads and writes worktree directories.
type WorktreeStore struct {
	queries *models.Queries
}

// NewWorktreeStore builds a worktree store over a pool or transaction.
func NewWorktreeStore(db store.DBTX) *WorktreeStore {
	return &WorktreeStore{queries: models.New(db)}
}

// List reads every directory, by directory, whether or not the watch carries it.
func (s *WorktreeStore) List(ctx context.Context) ([]Worktree, error) {
	rows, err := s.queries.ListWorktrees(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list worktrees: %w", err)
	}

	return xslice.Map(rows, worktreeFrom), nil
}

// ListSizes reads every measurement, by directory.
func (s *WorktreeStore) ListSizes(ctx context.Context) ([]Size, error) {
	worktrees, err := s.List(ctx)
	if err != nil {
		return nil, err
	}

	var sizes []Size
	for _, worktree := range worktrees {
		if worktree.Size != nil {
			sizes = append(sizes, *worktree.Size)
		}
	}

	return sizes, nil
}

// Upsert writes what the filesystem says of a directory over whatever it held, leaving its size
// alone and writing nothing when nothing moved.
func (s *WorktreeStore) Upsert(ctx context.Context, worktree Worktree) error {
	err := s.queries.UpsertWorktree(ctx, models.UpsertWorktreeParams{
		Dir:      worktree.Dir,
		Name:     filepath.Base(worktree.Dir),
		RepoDir:  worktree.RepoDir,
		Branch:   worktree.Branch,
		Readable: worktree.Readable,
		Mtime:    worktree.ModifiedAt,
	})
	if err != nil {
		return fmt.Errorf("failed to record the worktree %s: %w", worktree.Dir, err)
	}

	return nil
}

// RecordSize writes a directory's measurement, and nothing for a directory with no row.
func (s *WorktreeStore) RecordSize(ctx context.Context, size Size) error {
	params := models.RecordWorktreeSizeParams{
		Dir:        size.Dir,
		SizeBytes:  size.Bytes,
		MeasuredAt: &size.MeasuredAt,
	}
	if size.Error != "" {
		params.SizeError = &size.Error
	}

	if err := s.queries.RecordWorktreeSize(ctx, params); err != nil {
		return fmt.Errorf("failed to record the size of %s: %w", size.Dir, err)
	}

	return nil
}

// Delete forgets the directories named.
func (s *WorktreeStore) Delete(ctx context.Context, dirs []string) error {
	if len(dirs) == 0 {
		return nil
	}

	if err := s.queries.DeleteWorktrees(ctx, dirs); err != nil {
		return fmt.Errorf("failed to forget %d worktrees: %w", len(dirs), err)
	}

	return nil
}

// DeleteByName forgets the directories cut for the agents named.
func (s *WorktreeStore) DeleteByName(ctx context.Context, names []string) error {
	if len(names) == 0 {
		return nil
	}

	if err := s.queries.DeleteWorktreesByName(ctx, names); err != nil {
		return fmt.Errorf("failed to forget the worktrees of %d agents: %w", len(names), err)
	}

	return nil
}

func worktreeFrom(row *models.ListWorktreesRow) Worktree {
	worktree := Worktree{
		Dir:        row.Dir,
		RepoDir:    row.RepoDir,
		Branch:     row.Branch,
		Readable:   row.Readable,
		ModifiedAt: row.Mtime,
	}
	if row.MeasuredAt != nil {
		worktree.Size = &Size{
			Dir:        row.Dir,
			Bytes:      row.SizeBytes,
			MeasuredAt: *row.MeasuredAt,
		}
		if row.SizeError != nil {
			worktree.Size.Error = *row.SizeError
		}
	}

	return worktree
}
