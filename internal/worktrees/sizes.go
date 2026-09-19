package worktrees

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	worktreestore "weave/internal/store/worktree"

	"github.com/harrisoncramer/streamer"
)

// countWorkers is how many directories are counted together.
const countWorkers = 4

// countTimeout bounds one directory's count, since a dead network mount hangs rather than
// failing.
const countTimeout = 2 * time.Minute

type sizeStore interface {
	sizeLister
	Put(ctx context.Context, size worktreestore.Size) error
	Delete(ctx context.Context, dirs []string) error
}

// Counter walks the worktree directories and records how many bytes each holds.
type Counter struct {
	lister  *Lister
	sizes   sizeStore
	refresh time.Duration
	now     func() time.Time
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

// Count takes the directories whose count is missing or stale, and drops the counts of
// directories that are gone.
func (c *Counter) Count(ctx context.Context) error {
	dirs, err := c.lister.Dirs(ctx)
	if err != nil {
		return err
	}

	known, err := c.sizes.List(ctx)
	if err != nil {
		return err
	}

	present := map[string]bool{}
	var due []string
	for _, dir := range walk(dirs).paths {
		present[dir] = true
		if size, ok := known[dir]; !ok || c.now().Sub(size.CountedAt) >= c.refresh {
			due = append(due, dir)
		}
	}

	var gone []string
	for dir := range known {
		if !present[dir] {
			gone = append(gone, dir)
		}
	}
	if err := c.sizes.Delete(ctx, gone); err != nil {
		return err
	}

	return c.countAll(ctx, due)
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

	size, err := measure(ctx, dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to count %q: %w", dir, err)
	}

	return c.sizes.Put(ctx, worktreestore.Size{Dir: dir, Bytes: size, CountedAt: c.now()})
}

// measure is the sum of the sizes of every file under a directory, following no symlinks and
// skipping whatever vanishes while it is walked.
func measure(ctx context.Context, dir string) (int64, error) {
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

		info, err := entry.Info()
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}

		return nil
	})
	if err != nil {
		return 0, err
	}

	return total, nil
}
