// Package worktree stores the disk each worktree directory takes, as it was last measured.
package worktree

import (
	"context"
	"fmt"
	"time"

	"weave/db/models"
	"weave/internal/store"
	"weave/lib/xslice"
)

// Size is what one directory took when it was last walked, or why the walk failed.
type Size struct {
	Dir        string
	Bytes      int64
	MeasuredAt time.Time
	// Error is why the last walk failed, empty when it did not.
	Error string
}

// WorktreeStore reads and writes worktree sizes.
type WorktreeStore struct {
	queries *models.Queries
}

// NewWorktreeStore builds a size store over a pool or transaction.
func NewWorktreeStore(db store.DBTX) *WorktreeStore {
	return &WorktreeStore{queries: models.New(db)}
}

// ListSizes reads every measurement, by directory.
func (s *WorktreeStore) ListSizes(ctx context.Context) ([]Size, error) {
	rows, err := s.queries.ListWorktreeSizes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list worktree sizes: %w", err)
	}

	return xslice.Map(rows, func(row *models.WorktreeSize) Size {
		size := Size{
			Dir:        row.Dir,
			Bytes:      row.SizeBytes,
			MeasuredAt: row.MeasuredAt,
		}
		if row.Error != nil {
			size.Error = *row.Error
		}

		return size
	}), nil
}

// Upsert writes a directory's measurement over whatever it held.
func (s *WorktreeStore) Upsert(ctx context.Context, size Size) error {
	params := models.UpsertWorktreeSizeParams{
		Dir:        size.Dir,
		SizeBytes:  size.Bytes,
		MeasuredAt: size.MeasuredAt,
	}
	if size.Error != "" {
		params.Error = &size.Error
	}

	if err := s.queries.UpsertWorktreeSize(ctx, params); err != nil {
		return fmt.Errorf("failed to record the size of %s: %w", size.Dir, err)
	}

	return nil
}

// Delete forgets the measurements of the directories named.
func (s *WorktreeStore) Delete(ctx context.Context, dirs []string) error {
	if len(dirs) == 0 {
		return nil
	}

	if err := s.queries.DeleteWorktreeSizes(ctx, dirs); err != nil {
		return fmt.Errorf("failed to forget %d worktree sizes: %w", len(dirs), err)
	}

	return nil
}
