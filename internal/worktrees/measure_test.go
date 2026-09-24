package worktrees

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	worktreestore "weave/internal/store/worktree"
	"weave/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMeasureTakesTheUnmeasuredFirstThenTheStaleOldestFirst covers a pass walking at most eight
// directories, those with no size before those measured over an hour ago, oldest first, and
// leaving a fresh measurement alone.
func TestMeasureTakesTheUnmeasuredFirstThenTheStaleOldestFirst(t *testing.T) {
	f := newFixture(t)
	dirs := xslice.Map([]string{"aaaa", "bbbb", "cccc", "dddd", "eeee", "ffff", "gggg", "hhhh", "iiii", "jjjj"}, func(id string) string {
		return f.cut(t, id)
	})
	fresh, older, oldest := dirs[7], dirs[8], dirs[9]
	f.sizes.stored[fresh] = worktreestore.Size{
		Dir:        fresh,
		Bytes:      1,
		MeasuredAt: f.now.Add(-10 * time.Minute),
	}
	f.sizes.stored[older] = worktreestore.Size{
		Dir:        older,
		Bytes:      1,
		MeasuredAt: f.now.Add(-2 * time.Hour),
	}
	f.sizes.stored[oldest] = worktreestore.Size{
		Dir:        oldest,
		Bytes:      1,
		MeasuredAt: f.now.Add(-3 * time.Hour),
	}

	require.NoError(t, f.measurer().Measure(t.Context()))

	assert.Equal(t, append(dirs[:7:7], oldest), f.sizes.written)
	assert.Positive(t, f.sizes.stored[dirs[0]].Bytes)
	assert.Empty(t, f.sizes.stored[dirs[0]].Error)
	assert.Equal(t, f.now, f.sizes.stored[dirs[0]].MeasuredAt)
	assert.Equal(t, int64(1), f.sizes.stored[older].Bytes, "the newer stale directory waits for the next pass")

	require.NoError(t, f.measurer().Measure(t.Context()))

	assert.Equal(t, append(dirs[:7:7], oldest, older), f.sizes.written)
	assert.Equal(t, int64(1), f.sizes.stored[fresh].Bytes, "a fresh measurement is left alone")
}

// TestMeasureRecordsAFailedWalkAndDoesNotRetryIt covers a walk that fails being recorded with
// its error and the time, left alone on the next pass, and taken again once it is stale.
func TestMeasureRecordsAFailedWalkAndDoesNotRetryIt(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a directory whatever its mode")
	}

	f := newFixture(t)
	dir := f.cut(t, "aaaa")
	locked := filepath.Join(dir, "locked")
	require.NoError(t, os.Mkdir(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	require.NoError(t, f.measurer().Measure(t.Context()))

	require.Len(t, f.sizes.written, 1)
	recorded := f.sizes.stored[dir]
	assert.Zero(t, recorded.Bytes)
	assert.Contains(t, recorded.Error, "permission denied")
	assert.Equal(t, f.now, recorded.MeasuredAt)

	require.NoError(t, f.measurer().Measure(t.Context()))

	assert.Len(t, f.sizes.written, 1, "a failed walk is not taken again on the next pass")

	f.now = f.now.Add(2 * time.Hour)
	require.NoError(t, f.measurer().Measure(t.Context()))

	assert.Len(t, f.sizes.written, 2, "a failed walk is taken again once it is stale")
}

// TestMeasureForgetsADirectoryThatHasGone covers a stored row whose directory the listing no
// longer holds being deleted, unless a worktree directory could not be read at all.
func TestMeasureForgetsADirectoryThatHasGone(t *testing.T) {
	f := newFixture(t)
	kept := f.cut(t, "aaaa")
	gone := filepath.Join(f.shared, "gone")
	for _, dir := range []string{kept, gone} {
		f.sizes.stored[dir] = worktreestore.Size{
			Dir:        dir,
			Bytes:      1,
			MeasuredAt: f.now,
		}
	}

	unreadable := filepath.Join(filepath.Dir(f.shared), "not-a-dir")
	write(t, filepath.Dir(f.shared), "not-a-dir", "x\n")
	f.flows.dirs = []string{unreadable}
	require.NoError(t, f.measurer().Measure(t.Context()))

	assert.Empty(t, f.sizes.deleted, "nothing is forgotten while a worktree directory cannot be read")

	f.flows.dirs = nil
	require.NoError(t, f.measurer().Measure(t.Context()))

	assert.Equal(t, []string{gone}, f.sizes.deleted)
	assert.Contains(t, f.sizes.stored, kept)
	assert.NotContains(t, f.sizes.stored, gone)
}

// TestMeasureCarriesOnPastADirectoryItCannotRecord covers one refused write neither stopping
// the pass nor hiding the failure: the directories after it are still walked and the error
// comes back.
func TestMeasureCarriesOnPastADirectoryItCannotRecord(t *testing.T) {
	f := newFixture(t)
	refused := f.cut(t, "aaaa")
	after := f.cut(t, "bbbb")
	f.sizes.refused = map[string]error{refused: errors.New("invalid byte sequence for encoding \"UTF8\"")}

	err := f.measurer().Measure(t.Context())

	require.ErrorContains(t, err, "invalid byte sequence")
	assert.Equal(t, []string{after}, f.sizes.written)
	assert.Positive(t, f.sizes.stored[after].Bytes)
}

// TestMeasureForgetsADirectoryThatVanishesMidPass covers a directory the listing named that is
// gone by the time it is walked: its row is deleted and no error is recorded for it.
func TestMeasureForgetsADirectoryThatVanishesMidPass(t *testing.T) {
	f := newFixture(t)
	vanished := filepath.Join(f.shared, "vanished")
	f.sizes.stored[vanished] = worktreestore.Size{
		Dir:        vanished,
		Bytes:      1,
		MeasuredAt: f.now.Add(-2 * time.Hour),
	}
	measurer := NewMeasurer(NewMeasurerParams{
		Lister:    &fakeListing{listing: Listing{Worktrees: []Worktree{{Dir: vanished}}}},
		Claimed:   f.claims,
		Worktrees: f.sizes,
		Now:       func() time.Time { return f.now },
	})

	require.NoError(t, measurer.Measure(t.Context()))

	assert.Equal(t, []string{vanished}, f.sizes.deleted)
	assert.Empty(t, f.sizes.written)
	assert.NotContains(t, f.sizes.stored, vanished)
	assert.NotContains(t, f.sizes.records, vanished)
}

// TestMeasureRecordsWhatTheFilesystemSays covers every directory getting a row carrying its
// repository, branch, readability and mtime, whether or not this pass walks it.
func TestMeasureRecordsWhatTheFilesystemSays(t *testing.T) {
	f := newFixture(t)
	dirs := xslice.Map([]string{"aaaa", "bbbb", "cccc", "dddd", "eeee", "ffff", "gggg", "hhhh", "iiii"}, func(id string) string {
		return f.cut(t, id)
	})
	orphan := filepath.Join(f.shared, "orphan")
	require.NoError(t, os.Mkdir(orphan, 0o755))

	require.NoError(t, f.measurer().Measure(t.Context()))

	require.Len(t, f.sizes.records, len(dirs)+1, "a directory the pass does not walk still has its row")
	record := f.sizes.records[dirs[8]]
	assert.Equal(t, f.repo, record.RepoDir)
	assert.Equal(t, "agent/iiii", record.Branch)
	assert.True(t, record.Readable)
	require.NotNil(t, record.ModifiedAt)
	assert.Equal(t, time.UTC, record.ModifiedAt.Location())

	bare := f.sizes.records[orphan]
	assert.Empty(t, bare.RepoDir)
	assert.False(t, bare.Readable)
	assert.NotNil(t, bare.ModifiedAt, "a directory with no .git file takes its own mtime")
}

// TestMeasureLeavesOutWhatADeleteHasClaimed covers a directory whose agent is being deleted and
// an unowned one an unfinished delete names losing their rows, while an owned directory a delete
// names without claiming its agent keeps its own.
func TestMeasureLeavesOutWhatADeleteHasClaimed(t *testing.T) {
	f := newFixture(t)
	deleting := f.cut(t, "aaaa")
	named := f.cut(t, "bbbb")
	owned := f.cut(t, "cccc")
	kept := f.cut(t, "dddd")
	f.own("aaaa", "going", nil)
	f.owners.rows[0].DeletingAt = &f.now
	f.own("cccc", "live", nil)
	for _, dir := range []string{deleting, named, owned, kept} {
		f.sizes.records[dir] = worktreestore.Worktree{Dir: dir}
	}
	f.claims.dirs = []string{named, owned}

	require.NoError(t, f.measurer().Measure(t.Context()))

	assert.ElementsMatch(t, []string{deleting, named}, f.sizes.deleted)
	assert.NotContains(t, f.sizes.records, deleting)
	assert.NotContains(t, f.sizes.records, named)
	assert.Contains(t, f.sizes.records, owned)
	assert.Contains(t, f.sizes.records, kept)
	assert.NotContains(t, f.sizes.written, deleting, "a claimed directory is not walked")
	assert.NotContains(t, f.sizes.written, named)
}

// fakeListing answers with whatever listing the test names, on disk or not.
type fakeListing struct {
	listing Listing
}

func (l *fakeListing) List(context.Context) (*Listing, error) {
	return &l.listing, nil
}

// TestStorableText covers a NUL and a half rune being taken out of an error's text.
func TestStorableText(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "a NUL is dropped",
			text: "read a\x00b: denied",
			want: "read ab: denied",
		},
		{
			name: "a half rune is dropped",
			text: "read /trees/\xff: denied",
			want: "read /trees/: denied",
		},
		{
			name: "clean text is left alone",
			text: "read /trees/é: denied",
			want: "read /trees/é: denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, storableText(tt.text))
		})
	}
}
