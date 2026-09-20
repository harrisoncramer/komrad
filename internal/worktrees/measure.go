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
)

// measuresPerPass is how many directories one pass walks.
const measuresPerPass = 8

// measureDeadline is how long one walk may take before it is recorded as failed.
const measureDeadline = 30 * time.Second

// measureStaleAfter is how old a measurement is before a pass takes it again.
const measureStaleAfter = time.Hour

type pathLister interface {
	ListPaths(ctx context.Context) (Paths, error)
}

type sizeReadWriter interface {
	ListSizes(ctx context.Context) ([]worktreestore.Size, error)
	Upsert(ctx context.Context, size worktreestore.Size) error
	Delete(ctx context.Context, dirs []string) error
}

// Measurer walks the worktree directories a few at a time and records what each takes.
type Measurer struct {
	lister pathLister
	sizes  sizeReadWriter
	now    func() time.Time
}

type NewMeasurerParams struct {
	Lister pathLister
	Sizes  sizeReadWriter
	// Now is the clock, time.Now when unset.
	Now func() time.Time
}

// NewMeasurer builds a measurer over the lister and the size store.
func NewMeasurer(params NewMeasurerParams) *Measurer {
	now := params.Now
	if now == nil {
		now = time.Now
	}

	return &Measurer{
		lister: params.Lister,
		sizes:  params.Sizes,
		now:    now,
	}
}

// Measure forgets the directories that have gone, then walks the ones never measured and the
// ones measured longest ago, at most a few per pass, carrying on past one it cannot record.
func (m *Measurer) Measure(ctx context.Context) error {
	found, err := m.lister.ListPaths(ctx)
	if err != nil {
		return err
	}

	sizes, err := m.sizes.ListSizes(ctx)
	if err != nil {
		return err
	}

	stored := make(map[string]worktreestore.Size, len(sizes))
	for _, size := range sizes {
		stored[size.Dir] = size
	}

	if len(found.Unreadable) == 0 {
		if err := m.sizes.Delete(ctx, gone(found.Paths, stored)); err != nil {
			return err
		}
	}

	var failed []error
	for _, dir := range choose(found.Paths, stored, m.now()) {
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
		return m.sizes.Delete(ctx, []string{dir})
	}

	size := worktreestore.Size{Dir: dir, MeasuredAt: m.now()}
	if err != nil {
		size.Error = storableText(err.Error())
	} else {
		size.Bytes = bytes
	}

	return m.sizes.Upsert(ctx, size)
}

// storableText is an error's text as a text column will take it, without NULs or half runes.
func storableText(text string) string {
	return strings.ToValidUTF8(strings.ReplaceAll(text, "\x00", ""), "")
}

// gone is every stored directory the listing no longer holds.
func gone(paths []string, stored map[string]worktreestore.Size) []string {
	listed := make(map[string]bool, len(paths))
	for _, dir := range paths {
		listed[dir] = true
	}

	var dirs []string
	for dir := range stored {
		if !listed[dir] {
			dirs = append(dirs, dir)
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
