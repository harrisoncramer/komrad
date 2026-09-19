package worktrees

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	worktreestore "weave/internal/store/worktree"

	"github.com/harrisoncramer/streamer"
)

// countWorkers is how many directories are counted together.
const countWorkers = 4

// countTimeout bounds one directory's count.
const countTimeout = 2 * time.Minute

type sizeStore interface {
	sizeLister
	Put(ctx context.Context, size worktreestore.Size) error
	Delete(ctx context.Context, dirs []string) error
}

// Counter walks the worktree directories and records how many bytes each holds.
type Counter struct {
	lister   *Lister
	sizes    sizeStore
	refresh  time.Duration
	now      func() time.Time
	lastPass time.Time
}

type NewCounterParams struct {
	Lister *Lister
	Sizes  sizeStore
	// Refresh is how old a count may be before a pass takes it again.
	Refresh time.Duration
}

// NewCounter builds a counter over the lister and the size store.
func NewCounter(params NewCounterParams) *Counter {
	return &Counter{
		lister:  params.Lister,
		sizes:   params.Sizes,
		refresh: params.Refresh,
		now:     time.Now,
	}
}

// Count takes the directories that have no count, takes the stale ones again when a listing has
// been asked for since the last pass, and drops the counts of directories that are gone.
func (c *Counter) Count(ctx context.Context) error {
	dirs, err := c.lister.ListDirs(ctx)
	if err != nil {
		return err
	}

	known, err := c.sizes.List(ctx)
	if err != nil {
		return err
	}

	found := walk(dirs)
	refreshing := c.lister.LastListed().After(c.lastPass)

	present := map[string]bool{}
	var due []string
	for _, dir := range found.paths {
		present[dir] = true
		size, ok := known[dir]
		if !ok || (refreshing && c.now().Sub(size.CountedAt) >= c.refresh) {
			due = append(due, dir)
		}
	}

	var gone []string
	for dir := range known {
		if !present[dir] && !underAny(found.unreadable, dir) {
			gone = append(gone, dir)
		}
	}
	if err := c.sizes.Delete(ctx, gone); err != nil {
		return err
	}

	c.lastPass = c.now()

	return c.countAll(ctx, due)
}

// underAny reports whether a path sits immediately under one of the directories.
func underAny(dirs []string, path string) bool {
	return slices.Contains(dirs, filepath.Dir(path))
}

// counted is one directory's count, or why it has none.
type counted struct {
	dir string
	err error
}

func (c *Counter) countAll(ctx context.Context, dirs []string) error {
	counter, err := streamer.NewStreamer(streamer.NewStreamerParams[string, counted]{
		WorkerCount: countWorkers,
		Work: func(ctx context.Context, dir string) (counted, error) {
			return counted{dir: dir, err: c.count(ctx, dir)}, nil
		},
	})
	if err != nil {
		return fmt.Errorf("failed to build the size counter: %w", err)
	}

	input := make(chan string, len(dirs))
	for _, dir := range dirs {
		input <- dir
	}
	close(input)

	results, workErrs, err := counter.Stream(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to count worktree sizes: %w", err)
	}

	var failures []error
	for result := range results {
		if result.err != nil {
			failures = append(failures, result.err)
		}
	}
	for err := range workErrs {
		failures = append(failures, err)
	}

	return errors.Join(failures...)
}

func (c *Counter) count(ctx context.Context, dir string) error {
	ctx, cancel := context.WithTimeout(ctx, countTimeout)
	defer cancel()

	total, entries, err := measure(ctx, dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to count %q: %w", dir, err)
	}

	return c.sizes.Put(ctx, worktreestore.Size{Dir: dir, Bytes: total, CountedAt: c.now(), Entries: entries})
}

// measured is what one walk of a directory found.
type measured struct {
	total   int64
	entries []worktreestore.Entry
	err     error
}

// measure is the sum of the sizes of every file under a directory and what each top-level entry
// holds, following no symlink and skipping whatever vanishes while it is walked.
func measure(ctx context.Context, dir string) (int64, []worktreestore.Entry, error) {
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
	byEntry := map[string]*worktreestore.Entry{}
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
			top = &worktreestore.Entry{Path: head, Dir: entry.IsDir()}
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

	entries := make([]worktreestore.Entry, 0, len(order))
	for _, head := range order {
		entries = append(entries, *byEntry[head])
	}
	slices.SortStableFunc(entries, func(a, b worktreestore.Entry) int { return cmp.Compare(b.Bytes, a.Bytes) })

	return measured{total: total, entries: entries}
}
