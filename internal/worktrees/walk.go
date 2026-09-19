package worktrees

import (
	"cmp"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Entry is one top-level entry of a directory with what it holds.
type Entry struct {
	Path  string
	Bytes int64
	Dir   bool
}

// measured is what one walk of a directory found.
type measured struct {
	total   int64
	entries []Entry
	err     error
}

// measure is the sum of the sizes of every file under a directory and what each top-level entry
// holds, following no symlink and skipping whatever vanishes while it is walked.
func measure(ctx context.Context, dir string) (int64, []Entry, error) {
	// A walk stuck in a syscall is abandoned when the context ends.
	done := make(chan measured, 1)
	go func() { done <- walkSizes(ctx, dir) }()

	select {
	case result := <-done:
		return result.total, result.entries, result.err
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	}
}

func walkSizes(ctx context.Context, dir string) measured {
	byEntry := map[string]*Entry{}
	var order []string
	var total int64

	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, os.ErrNotExist) && path != dir {
			return nil
		}
		if err != nil {
			return err
		}
		if path == dir {
			return nil
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		head, _, _ := strings.Cut(rel, string(filepath.Separator))
		top, ok := byEntry[head]
		if !ok {
			top = &Entry{Path: head, Dir: entry.IsDir()}
			byEntry[head] = top
			order = append(order, head)
		}

		info, err := entry.Info()
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			total += info.Size()
			top.Bytes += info.Size()
		}

		return nil
	})
	if err != nil {
		return measured{err: err}
	}

	entries := make([]Entry, 0, len(order))
	for _, head := range order {
		entries = append(entries, *byEntry[head])
	}
	slices.SortStableFunc(entries, func(a, b Entry) int { return cmp.Compare(b.Bytes, a.Bytes) })

	return measured{total: total, entries: entries}
}
