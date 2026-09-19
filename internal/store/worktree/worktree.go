// Package worktree stores how big each worktree directory was when the service last counted it.
package worktree

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"weave/db/models"
	"weave/internal/store"
)

// Entry is one top-level entry of a directory as the count passed over it.
type Entry struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	Dir   bool   `json:"dir"`
}

// Size is one directory's count.
type Size struct {
	Dir       string
	Bytes     int64
	CountedAt time.Time
	// Entries are the top-level entries, biggest first.
	Entries []Entry
}

type Store struct {
	queries *models.Queries
}

// New builds a size store over a pool or transaction.
func New(db store.DBTX) *Store {
	return &Store{queries: models.New(db)}
}

// Put records a directory's count, replacing an earlier one.
func (s *Store) Put(ctx context.Context, size Size) error {
	entries, err := json.Marshal(size.Entries)
	if err != nil {
		return fmt.Errorf("failed to encode the entries of %q: %w", size.Dir, err)
	}

	err = s.queries.UpsertWorktreeSize(ctx, models.UpsertWorktreeSizeParams{
		Dir:       size.Dir,
		SizeBytes: size.Bytes,
		CountedAt: size.CountedAt,
		Entries:   entries,
	})
	if err != nil {
		return fmt.Errorf("failed to record the size of %q: %w", size.Dir, err)
	}

	return nil
}

// List is every count, keyed by directory.
func (s *Store) List(ctx context.Context) (map[string]Size, error) {
	rows, err := s.queries.ListWorktreeSizes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list worktree sizes: %w", err)
	}

	sizes := make(map[string]Size, len(rows))
	for _, row := range rows {
		size := Size{Dir: row.Dir, Bytes: row.SizeBytes, CountedAt: row.CountedAt}
		if err := json.Unmarshal(row.Entries, &size.Entries); err != nil {
			return nil, fmt.Errorf("failed to read the entries of %q: %w", row.Dir, err)
		}

		sizes[row.Dir] = size
	}

	return sizes, nil
}

// Delete drops the counts of directories that are gone.
func (s *Store) Delete(ctx context.Context, dirs []string) error {
	if len(dirs) == 0 {
		return nil
	}

	if err := s.queries.DeleteWorktreeSizes(ctx, dirs); err != nil {
		return fmt.Errorf("failed to drop %d worktree sizes: %w", len(dirs), err)
	}

	return nil
}
