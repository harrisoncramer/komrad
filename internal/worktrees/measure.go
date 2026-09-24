package worktrees

import (
	"cmp"
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"time"

	worktreestore "weave/internal/store/worktree"
	"weave/lib/xptr"
	"weave/lib/xslice"
)

// measuresPerPass is how many directories one pass walks.
const measuresPerPass = 8

// measureDeadline is how long one walk may take before it is recorded as failed.
const measureDeadline = 30 * time.Second

// measureStaleAfter is how old a measurement is before a pass takes it again.
const measureStaleAfter = time.Hour

type worktreeLister interface {
	List(ctx context.Context) (*Listing, error)
}

type claimedDirLister interface {
	ListClaimedDirs(ctx context.Context) ([]string, error)
}

type worktreeReadWriter interface {
	List(ctx context.Context) ([]worktreestore.Worktree, error)
	Upsert(ctx context.Context, worktree worktreestore.Worktree) error
	RecordSize(ctx context.Context, size worktreestore.Size) error
	Delete(ctx context.Context, dirs []string) error
}

type NewMeasurerParams struct {
	Lister    worktreeLister
	Claimed   claimedDirLister
	Worktrees worktreeReadWriter
	// Now is the clock, time.Now when unset.
	Now func() time.Time
}

// Measurer records the worktree directories and walks a few of them a pass for their size.
type Measurer struct {
	lister    worktreeLister
	claimed   claimedDirLister
	worktrees worktreeReadWriter
	now       func() time.Time
}

// NewMeasurer builds a measurer over the lister, the claims and the worktree store.
func NewMeasurer(params NewMeasurerParams) *Measurer {
	now := params.Now
	if now == nil {
		now = time.Now
	}

	return &Measurer{
		lister:    params.Lister,
		claimed:   params.Claimed,
		worktrees: params.Worktrees,
		now:       now,
	}
}

// Measure records every directory no delete names, forgets the rest, and walks the few most in
// need of a size, carrying on past one it cannot record.
func (m *Measurer) Measure(ctx context.Context) error {
	listing, err := m.lister.List(ctx)
	if err != nil {
		return err
	}

	claimed, err := m.claimed.ListClaimedDirs(ctx)
	if err != nil {
		return err
	}

	stored, err := m.worktrees.List(ctx)
	if err != nil {
		return err
	}

	present := unnamed(listing.Worktrees, claimed)
	if err := m.worktrees.Delete(ctx, forgotten(listing, present, stored)); err != nil {
		return err
	}

	var failed []error
	for _, worktree := range present {
		if err := m.worktrees.Upsert(ctx, recordOf(worktree)); err != nil {
			failed = append(failed, err)
		}
	}

	sizes := make(map[string]worktreestore.Size, len(stored))
	for _, worktree := range stored {
		if worktree.Size != nil {
			sizes[worktree.Dir] = *worktree.Size
		}
	}

	dirs := xslice.Map(present, func(worktree Worktree) string { return worktree.Dir })
	for _, dir := range choose(dirs, sizes, m.now()) {
		if err := m.measure(ctx, dir); err != nil {
			if ctx.Err() != nil {
				return err
			}

			failed = append(failed, err)
		}
	}

	return errors.Join(failed...)
}

// measure walks one directory under the deadline and records the size or the failure, or
// forgets a directory that has gone.
func (m *Measurer) measure(ctx context.Context, dir string) error {
	walkCtx, cancel := context.WithTimeout(ctx, measureDeadline)
	bytes, _, err := measure(walkCtx, dir)
	cancel()

	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, os.ErrNotExist) {
		return m.worktrees.Delete(ctx, []string{dir})
	}

	size := worktreestore.Size{Dir: dir, MeasuredAt: m.now()}
	if err != nil {
		size.Error = storableText(err.Error())
	} else {
		size.Bytes = bytes
	}

	return m.worktrees.RecordSize(ctx, size)
}

// recordOf is what the filesystem says of a directory, as its row holds it.
func recordOf(worktree Worktree) worktreestore.Worktree {
	record := worktreestore.Worktree{
		Dir:      worktree.Dir,
		RepoDir:  worktree.RepoDir,
		Branch:   worktree.Branch,
		Readable: worktree.Readable,
	}
	if !worktree.ModifiedAt.IsZero() {
		record.ModifiedAt = xptr.Ptr(worktree.ModifiedAt.UTC())
	}

	return record
}

// storableText is an error's text as a text column will take it, without NULs or half runes.
func storableText(text string) string {
	return strings.ToValidUTF8(strings.ReplaceAll(text, "\x00", ""), "")
}

// forgotten is every stored directory a delete names or that has gone, a gone one counting only
// while every worktree directory could be read.
func forgotten(listing *Listing, present []Worktree, stored []worktreestore.Worktree) []string {
	kept := make(map[string]bool, len(present))
	for _, worktree := range present {
		kept[worktree.Dir] = true
	}

	onDisk := make(map[string]bool, len(listing.Worktrees))
	for _, worktree := range listing.Worktrees {
		onDisk[worktree.Dir] = true
	}

	var dirs []string
	for _, worktree := range stored {
		switch {
		case kept[worktree.Dir]:
		case onDisk[worktree.Dir], len(listing.UnreadableDirs) == 0:
			dirs = append(dirs, worktree.Dir)
		}
	}
	slices.Sort(dirs)

	return dirs
}

// choose is the directories a pass walks: the unmeasured first, in listing order, then the
// stale ones oldest first, cut to what one pass takes.
func choose(paths []string, stored map[string]worktreestore.Size, now time.Time) []string {
	var unmeasured, stale []string
	for _, dir := range paths {
		size, ok := stored[dir]
		switch {
		case !ok:
			unmeasured = append(unmeasured, dir)
		case now.Sub(size.MeasuredAt) >= measureStaleAfter:
			stale = append(stale, dir)
		}
	}
	slices.SortStableFunc(stale, func(a, b string) int {
		return cmp.Compare(stored[a].MeasuredAt.UnixNano(), stored[b].MeasuredAt.UnixNano())
	})

	chosen := slices.Concat(unmeasured, stale)

	return chosen[:min(len(chosen), measuresPerPass)]
}
