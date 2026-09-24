package worktrees

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	agentstore "weave/internal/store/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMeasureTakesTheUnmeasuredFirstThenTheStaleOldestFirst covers a pass walking at most eight
// directories, those with no size before those measured over an hour ago, oldest first, and
// leaving a fresh measurement alone.
func TestMeasureTakesTheUnmeasuredFirstThenTheStaleOldestFirst(t *testing.T) {
	f := newFixture(t)
	dirs := make([]string, 10)
	for i := range dirs {
		dirs[i] = f.cut(t)
	}
	slices.Sort(dirs)
	fresh, older, oldest := dirs[7], dirs[8], dirs[9]
	f.measured(t, fresh, f.now.Add(-10*time.Minute))
	f.measured(t, older, f.now.Add(-2*time.Hour))
	f.measured(t, oldest, f.now.Add(-3*time.Hour))

	require.NoError(t, f.measurer().Measure(t.Context()))

	assert.ElementsMatch(t, append(dirs[:7:7], oldest), f.measuredAt(t, f.now))

	f.now = f.now.Add(time.Minute)
	require.NoError(t, f.measurer().Measure(t.Context()))

	assert.Equal(t, []string{older}, f.measuredAt(t, f.now), "the newer stale directory waits for the next pass")
	assert.Equal(t, int64(1), f.stored(t)[fresh].Size.Bytes, "a fresh measurement is left alone")
}

func (f *fixture) measuredAt(t *testing.T, at time.Time) []string {
	t.Helper()

	var dirs []string
	for dir, worktree := range f.stored(t) {
		if worktree.Size != nil && worktree.Size.MeasuredAt.Equal(at) {
			dirs = append(dirs, dir)
		}
	}

	return dirs
}

// TestMeasureRecordsAFailedWalkAndDoesNotRetryIt covers a walk that fails being recorded with
// its error and the time, left alone on the next pass, and taken again once it is stale.
func TestMeasureRecordsAFailedWalkAndDoesNotRetryIt(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a directory whatever its mode")
	}

	f := newFixture(t)
	dir := f.cut(t)
	locked := filepath.Join(dir, "locked")
	require.NoError(t, os.Mkdir(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	failedAt := f.now

	require.NoError(t, f.measurer().Measure(t.Context()))

	recorded := f.stored(t)[dir].Size
	require.NotNil(t, recorded)
	assert.Zero(t, recorded.Bytes)
	assert.Contains(t, recorded.Error, "permission denied")
	assert.True(t, recorded.MeasuredAt.Equal(failedAt))

	f.now = f.now.Add(time.Minute)
	require.NoError(t, f.measurer().Measure(t.Context()))

	assert.True(t, f.stored(t)[dir].Size.MeasuredAt.Equal(failedAt), "a failed walk is not taken again on the next pass")

	f.now = f.now.Add(2 * time.Hour)
	require.NoError(t, f.measurer().Measure(t.Context()))

	assert.True(t, f.stored(t)[dir].Size.MeasuredAt.Equal(f.now), "a failed walk is taken again once it is stale")
}

// TestMeasureForgetsADirectoryThatHasGone covers a stored row whose directory the listing no
// longer holds being deleted, unless a worktree directory could not be read at all.
func TestMeasureForgetsADirectoryThatHasGone(t *testing.T) {
	f := newFixture(t)
	kept := f.cut(t)
	gone := filepath.Join(f.shared, "gone")
	f.measured(t, kept, f.now)
	f.measured(t, gone, f.now)

	unreadable := filepath.Join(filepath.Dir(f.shared), "not-a-dir")
	write(t, filepath.Dir(f.shared), "not-a-dir", "x\n")
	blind := NewMeasurer(NewMeasurerParams{
		Lister:    f.listerOver(unreadable),
		Claimed:   f.claims,
		Worktrees: f.worktrees,
		Now:       func() time.Time { return f.now },
	})
	require.NoError(t, blind.Measure(t.Context()))

	assert.Contains(t, f.stored(t), gone, "nothing is forgotten while a worktree directory cannot be read")

	require.NoError(t, f.measurer().Measure(t.Context()))

	assert.Contains(t, f.stored(t), kept)
	assert.NotContains(t, f.stored(t), gone)
}

// TestMeasureCarriesOnPastADirectoryItCannotRecord covers one refused write neither stopping
// the pass nor hiding the failure: the directories after it are still walked and the error
// comes back.
func TestMeasureCarriesOnPastADirectoryItCannotRecord(t *testing.T) {
	f := newFixture(t)
	after := f.cut(t)
	measurer := NewMeasurer(NewMeasurerParams{
		Lister: &fakeListing{listing: Listing{Worktrees: []Worktree{
			{Dir: filepath.Join(f.shared, "nul\x00")},
			{Dir: after},
		}}},
		Claimed:   f.claims,
		Worktrees: f.worktrees,
		Now:       func() time.Time { return f.now },
	})

	err := measurer.Measure(t.Context())

	require.ErrorContains(t, err, "invalid byte sequence")
	require.NotNil(t, f.stored(t)[after].Size)
	assert.Positive(t, f.stored(t)[after].Size.Bytes)
}

// TestMeasureForgetsADirectoryThatVanishesMidPass covers a directory the listing named that is
// gone by the time it is walked: its row is deleted and no error is recorded for it.
func TestMeasureForgetsADirectoryThatVanishesMidPass(t *testing.T) {
	f := newFixture(t)
	vanished := filepath.Join(f.shared, "vanished")
	f.measured(t, vanished, f.now.Add(-2*time.Hour))
	measurer := NewMeasurer(NewMeasurerParams{
		Lister:    &fakeListing{listing: Listing{Worktrees: []Worktree{{Dir: vanished}}}},
		Claimed:   f.claims,
		Worktrees: f.worktrees,
		Now:       func() time.Time { return f.now },
	})

	require.NoError(t, measurer.Measure(t.Context()))

	assert.NotContains(t, f.stored(t), vanished)
}

// TestMeasureRecordsWhatTheFilesystemSays covers every directory getting a row carrying its
// repository, branch, readability and mtime, whether or not this pass walks it.
func TestMeasureRecordsWhatTheFilesystemSays(t *testing.T) {
	f := newFixture(t)
	dirs := make([]string, measuresPerPass+1)
	for i := range dirs {
		dirs[i] = f.cut(t)
	}
	orphan := filepath.Join(f.shared, "orphan")
	require.NoError(t, os.Mkdir(orphan, 0o755))

	require.NoError(t, f.measurer().Measure(t.Context()))

	stored := f.stored(t)
	require.Len(t, stored, len(dirs)+1, "a directory the pass does not walk still has its row")
	record := stored[dirs[0]]
	assert.Equal(t, f.repo, record.RepoDir)
	assert.Equal(t, "agent/"+filepath.Base(dirs[0]), record.Branch)
	assert.True(t, record.Readable)
	assert.NotNil(t, record.ModifiedAt)

	bare := stored[orphan]
	assert.Empty(t, bare.RepoDir)
	assert.False(t, bare.Readable)
	assert.NotNil(t, bare.ModifiedAt, "a directory with no .git file takes its own mtime")
}

// TestMeasureLeavesOutWhatADeleteNames covers an unowned directory a delete names losing its row.
func TestMeasureLeavesOutWhatADeleteNames(t *testing.T) {
	f := newFixture(t)
	deleting := f.cut(t)
	named := f.cut(t)
	owned := f.cut(t)
	kept := f.cut(t)
	going := f.own(t, deleting, agentstore.Agent{Name: "going"})
	require.NoError(t, f.stores.Agents.MarkDeleting(t.Context(), []string{going.ID}))
	f.own(t, owned, agentstore.Agent{Name: "live"})
	for _, dir := range []string{deleting, named, owned, kept} {
		f.measured(t, dir, f.now.Add(-2*time.Hour))
	}
	f.claims.dirs = []string{named, owned}

	require.NoError(t, f.measurer().Measure(t.Context()))

	stored := f.stored(t)
	assert.NotContains(t, stored, named)
	assert.Contains(t, stored, deleting, "the watch leaves this one out, so the row is kept for a failed delete")
	assert.Contains(t, stored, owned)
	assert.Contains(t, stored, kept)
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
