package worktrees

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	agentstore "komrad/internal/store/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMeasureTakesTheUnmeasuredFirstThenTheStaleOldestFirst covers a pass walking at most eight
// directories, those with no size before those measured over an hour ago, oldest first, and
// leaving a fresh measurement alone.
func TestMeasureTakesTheUnmeasuredFirstThenTheStaleOldestFirst(t *testing.T) {
	fixture := newFixture(t)
	dirs := make([]string, 10)
	for i := range dirs {
		dirs[i] = fixture.cut(t)
	}
	slices.Sort(dirs)
	fresh, older, oldest := dirs[7], dirs[8], dirs[9]
	fixture.recordSize(t, fresh, fixture.now.Add(-10*time.Minute))
	fixture.recordSize(t, older, fixture.now.Add(-2*time.Hour))
	fixture.recordSize(t, oldest, fixture.now.Add(-3*time.Hour))

	require.NoError(t, fixture.newMeasurer().Measure(t.Context()))

	assert.ElementsMatch(t, append(dirs[:7:7], oldest), fixture.listMeasuredAt(t, fixture.now))

	fixture.now = fixture.now.Add(time.Minute)
	require.NoError(t, fixture.newMeasurer().Measure(t.Context()))

	assert.Equal(t, []string{older}, fixture.listMeasuredAt(t, fixture.now), "the newer stale directory waits for the next pass")
	assert.Equal(t, int64(1), fixture.readSize(t, fresh).Bytes, "a fresh measurement is left alone")
}

// TestMeasureRecordsAFailedWalkAndDoesNotRetryIt covers a walk that fails being recorded with
// its error and the time, left alone on the next pass, and taken again once it is stale.
func TestMeasureRecordsAFailedWalkAndDoesNotRetryIt(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a directory whatever its mode")
	}

	fixture := newFixture(t)
	dir := fixture.cut(t)
	locked := filepath.Join(dir, "locked")
	require.NoError(t, os.Mkdir(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	failedAt := fixture.now

	require.NoError(t, fixture.newMeasurer().Measure(t.Context()))

	recorded := fixture.readSize(t, dir)
	assert.Zero(t, recorded.Bytes)
	assert.Contains(t, recorded.Error, "permission denied")
	assert.True(t, recorded.MeasuredAt.Equal(failedAt))

	fixture.now = fixture.now.Add(time.Minute)
	require.NoError(t, fixture.newMeasurer().Measure(t.Context()))

	assert.True(t, fixture.readSize(t, dir).MeasuredAt.Equal(failedAt), "a failed walk is not taken again on the next pass")

	fixture.now = fixture.now.Add(2 * time.Hour)
	require.NoError(t, fixture.newMeasurer().Measure(t.Context()))

	assert.True(t, fixture.readSize(t, dir).MeasuredAt.Equal(fixture.now), "a failed walk is taken again once it is stale")
}

// TestMeasureForgetsADirectoryThatHasGone covers a stored row whose directory the listing no
// longer holds being deleted, unless a worktree directory could not be read at all.
func TestMeasureForgetsADirectoryThatHasGone(t *testing.T) {
	fixture := newFixture(t)
	kept := fixture.cut(t)
	gone := filepath.Join(fixture.shared, "gone")
	fixture.recordSize(t, kept, fixture.now)
	fixture.recordSize(t, gone, fixture.now)

	unreadable := filepath.Join(filepath.Dir(fixture.shared), "not-a-dir")
	write(t, filepath.Dir(fixture.shared), "not-a-dir", "x\n")
	blind := fixture.newMeasurerOver(fixture.newListerOver(unreadable))
	require.NoError(t, blind.Measure(t.Context()))

	assert.Contains(t, fixture.listStored(t), gone, "nothing is forgotten while a worktree directory cannot be read")

	require.NoError(t, fixture.newMeasurer().Measure(t.Context()))

	assert.Contains(t, fixture.listStored(t), kept)
	assert.NotContains(t, fixture.listStored(t), gone)
}

// TestMeasureCarriesOnPastADirectoryItCannotRecord covers one refused write neither stopping
// the pass nor hiding the failure: the directories after it are still walked and the error
// comes back.
func TestMeasureCarriesOnPastADirectoryItCannotRecord(t *testing.T) {
	fixture := newFixture(t)
	after := fixture.cut(t)
	measurer := fixture.newMeasurerOver(&fakeWorktreeLister{listing: Listing{Worktrees: []Worktree{
		{Dir: filepath.Join(fixture.shared, "nul\x00")},
		{Dir: after},
	}}})

	err := measurer.Measure(t.Context())

	require.ErrorContains(t, err, "invalid byte sequence")
	assert.Positive(t, fixture.readSize(t, after).Bytes)
}

// TestMeasureForgetsADirectoryThatVanishesMidPass covers a directory the listing named that is
// gone by the time it is walked: its row is deleted and no error is recorded for it.
func TestMeasureForgetsADirectoryThatVanishesMidPass(t *testing.T) {
	fixture := newFixture(t)
	vanished := filepath.Join(fixture.shared, "vanished")
	fixture.recordSize(t, vanished, fixture.now.Add(-2*time.Hour))
	measurer := fixture.newMeasurerOver(&fakeWorktreeLister{listing: Listing{Worktrees: []Worktree{{Dir: vanished}}}})

	require.NoError(t, measurer.Measure(t.Context()))

	assert.NotContains(t, fixture.listStored(t), vanished)
}

// TestMeasureRecordsWhatTheFilesystemSays covers every directory getting a row carrying its
// repository, branch, readability and mtime, whether or not this pass walks it.
func TestMeasureRecordsWhatTheFilesystemSays(t *testing.T) {
	fixture := newFixture(t)
	dirs := make([]string, measuresPerPass+1)
	for i := range dirs {
		dirs[i] = fixture.cut(t)
	}
	orphan := filepath.Join(fixture.shared, "orphan")
	require.NoError(t, os.Mkdir(orphan, 0o755))

	require.NoError(t, fixture.newMeasurer().Measure(t.Context()))

	stored := fixture.listStored(t)
	require.Len(t, stored, len(dirs)+1, "a directory the pass does not walk still has its row")
	record := stored[dirs[0]]
	assert.Equal(t, fixture.repo, record.RepoDir)
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
	fixture := newFixture(t)
	deleting := fixture.cut(t)
	named := fixture.cut(t)
	owned := fixture.cut(t)
	kept := fixture.cut(t)
	going := fixture.own(t, deleting, agentstore.Agent{Name: "going"})
	require.NoError(t, fixture.stores.Agents.MarkDeleting(t.Context(), []string{going.ID}))
	fixture.own(t, owned, agentstore.Agent{Name: "live"})
	for _, dir := range []string{deleting, named, owned, kept} {
		fixture.recordSize(t, dir, fixture.now.Add(-2*time.Hour))
	}
	fixture.claims.dirs = []string{named, owned}

	require.NoError(t, fixture.newMeasurer().Measure(t.Context()))

	stored := fixture.listStored(t)
	assert.NotContains(t, stored, named)
	assert.Contains(t, stored, deleting, "the watch leaves this one out, so the row is kept for a failed delete")
	assert.Contains(t, stored, owned)
	assert.Contains(t, stored, kept)
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
