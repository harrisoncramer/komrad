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
	"sync"
	"sync/atomic"
	"syscall"

	"golang.org/x/sync/errgroup"
)

// workers is how many directories a walk reads at once.
const workers = 8

// blockSize is the unit st_blocks counts in.
const blockSize = 512

// Entry is one top-level entry of a directory with what it holds.
type Entry struct {
	Path  string
	Bytes int64
	Dir   bool
}

// inode names one file across the links that share it.
type inode struct {
	dev int64
	ino uint64
}

type linked struct {
	bytes int64
	nlink int64
	met   int64
}

type links struct {
	mu    sync.Mutex
	files map[inode]*linked
}

type measurement struct {
	bytes   int64
	share   int64
	entries []Entry
}

// walker sums the disk under one top-level entry, reading its directories on a pool of its own.
type walker struct {
	group *errgroup.Group
	seen  *links
	sum   *atomic.Int64
}

func (l *links) meet(key inode, bytes, nlink int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	file, ok := l.files[key]
	if !ok {
		file = &linked{bytes: bytes, nlink: nlink}
		l.files[key] = file
	}
	file.met++

	return !ok
}

func (l *links) calculateShare(total int64) int64 {
	for _, file := range l.files {
		total -= file.bytes - file.bytes*min(file.met, file.nlink)/file.nlink
	}

	return total
}

// measure is the disk taken by every file under a directory and by each top-level entry,
// following no symlink and skipping whatever vanishes while it is walked.
func measure(ctx context.Context, dir string) (*measurement, error) {
	tops, err := readDir(ctx, dir)
	if err != nil {
		return nil, err
	}

	seen := &links{files: map[inode]*linked{}}
	var total int64
	entries := make([]Entry, 0, len(tops))
	for _, top := range tops {
		var sum atomic.Int64
		w := walker{seen: seen, sum: &sum}
		if err := w.walkTop(ctx, filepath.Join(dir, top.Name()), top); err != nil {
			return nil, err
		}
		total += sum.Load()
		entries = append(entries, Entry{
			Path:  top.Name(),
			Bytes: sum.Load(),
			Dir:   top.IsDir(),
		})
	}
	slices.SortStableFunc(entries, func(a, b Entry) int {
		return cmp.Compare(b.Bytes, a.Bytes)
	})

	return &measurement{
		bytes:   total,
		share:   seen.calculateShare(total),
		entries: entries,
	}, nil
}

// walkTop counts a top-level file, or reads a top-level directory to the end on a fresh pool.
func (w walker) walkTop(ctx context.Context, path string, entry fs.DirEntry) error {
	if !entry.IsDir() {
		return w.count(entry)
	}

	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(workers)
	w.group = group
	group.Go(func() error {
		return w.walk(ctx, path)
	})

	return group.Wait()
}

// enter counts a file, or walks a directory on a free worker, or on the caller when none is free.
func (w walker) enter(ctx context.Context, path string, entry fs.DirEntry) error {
	if !entry.IsDir() {
		return w.count(entry)
	}
	if w.group.TryGo(func() error {
		return w.walk(ctx, path)
	}) {
		return nil
	}

	return w.walk(ctx, path)
}

func (w walker) walk(ctx context.Context, path string) error {
	entries, err := readDir(ctx, path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if err := w.enter(ctx, filepath.Join(path, entry.Name()), entry); err != nil {
			return err
		}
	}

	return nil
}

// count adds a regular file's blocks to the sum, once across the links that share it.
func (w walker) count(entry fs.DirEntry) error {
	info, err := entry.Info()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("stat %s: no stat_t behind %T", entry.Name(), info.Sys())
	}
	bytes := stat.Blocks * blockSize
	if stat.Nlink > 1 {
		if !w.seen.meet(inode{dev: int64(stat.Dev), ino: stat.Ino}, bytes, int64(stat.Nlink)) {
			return nil
		}
	}
	w.sum.Add(bytes)

	return nil
}

// readDir lists a directory unless the context has ended.
func readDir(ctx context.Context, path string) ([]fs.DirEntry, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	return os.ReadDir(path)
}
