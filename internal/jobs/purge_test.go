package jobs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	agentstore "weave/internal/store/agent"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPurgeArchiveWorker covers which of the archive a pass takes, since the horizon the job
// carries and the ids the delete is handed have to line up.
func TestPurgeArchiveWorker(t *testing.T) {
	tests := []struct {
		name string
		// after is the horizon the job carries, zero for a config asking for no purge.
		after time.Duration
		// archived is how long ago each seeded agent was put away.
		archived []time.Duration
		// wantDeleted is how many of them the delete should have been handed.
		wantDeleted int
		// wantVacuum says the tables should have been vacuumed.
		wantVacuum bool
	}{
		{
			name:     "a horizon of zero is never, so nothing is even read",
			archived: []time.Duration{100 * time.Hour},
		},
		{
			name:        "an agent archived longer ago than the horizon goes",
			after:       24 * time.Hour,
			archived:    []time.Duration{48 * time.Hour},
			wantDeleted: 1,
			wantVacuum:  true,
		},
		{
			name:     "one archived inside the horizon stays, and nothing is vacuumed",
			after:    24 * time.Hour,
			archived: []time.Duration{time.Hour},
		},
		{
			name:        "only the ones past the horizon go",
			after:       24 * time.Hour,
			archived:    []time.Duration{48 * time.Hour, time.Hour, 72 * time.Hour},
			wantDeleted: 2,
			wantVacuum:  true,
		},
		{
			name:        "an archive larger than one batch is taken in several",
			after:       time.Hour,
			archived:    buildArchivedAges(purgeBatchSize + 5),
			wantDeleted: purgeBatchSize + 5,
			wantVacuum:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			archive := newFakeArchive(tt.archived...)
			purger := &fakePurger{archive: archive}
			tables := &fakeTables{}

			worker := NewPurgeArchiveWorker(PurgeArchiveParams{
				Archive: archive,
				Purger:  purger,
				Tables:  tables,
				Logger:  newDiscardLogger(),
			})

			require.NoError(t, worker.Work(t.Context(), &river.Job[PurgeArchiveArgs]{
				Args: PurgeArchiveArgs{After: tt.after},
			}))

			assert.Len(t, purger.deleted, tt.wantDeleted)
			assert.Equal(t, tt.wantVacuum, tables.vacuums > 0, "whether the pass vacuumed")

			if tt.after == 0 {
				assert.Zero(t, archive.reads, "a horizon of zero reads nothing at all")
			}
		})
	}
}

// TestPurgeSkipsAnAgentRestoredUnderIt covers the one race the delete cannot see, since it
// is keyed on ids alone and an agent that is live again must not reach it.
func TestPurgeSkipsAnAgentRestoredUnderIt(t *testing.T) {
	archive := newFakeArchive(48*time.Hour, 48*time.Hour)
	purger := &fakePurger{archive: archive}

	restored := archive.ids()[0]
	archive.beforeReread = func() { archive.restore(restored) }

	worker := NewPurgeArchiveWorker(PurgeArchiveParams{
		Archive: archive,
		Purger:  purger,
		Tables:  &fakeTables{},
		Logger:  newDiscardLogger(),
	})

	require.NoError(t, worker.Work(t.Context(), &river.Job[PurgeArchiveArgs]{
		Args: PurgeArchiveArgs{After: 24 * time.Hour},
	}))

	require.Len(t, purger.deleted, 1)
	assert.NotEqual(t, restored, purger.deleted[0], "the agent that came back was left alone")
}

// TestPurgeReportsAFailedDelete covers a pass that could not finish, whose failure has to
// reach River rather than being swallowed by the vacuum that follows it.
func TestPurgeReportsAFailedDelete(t *testing.T) {
	archive := newFakeArchive(48 * time.Hour)
	purger := &fakePurger{archive: archive, err: errors.New("the pane would not die")}
	tables := &fakeTables{}

	worker := NewPurgeArchiveWorker(PurgeArchiveParams{
		Archive: archive,
		Purger:  purger,
		Tables:  tables,
		Logger:  newDiscardLogger(),
	})

	err := worker.Work(t.Context(), &river.Job[PurgeArchiveArgs]{
		Args: PurgeArchiveArgs{After: 24 * time.Hour},
	})

	require.Error(t, err)
	assert.Zero(t, tables.vacuums, "a pass that removed nothing has nothing to vacuum")
}

// buildArchivedAges is one age per agent, all well past any horizon a test sets.
func buildArchivedAges(count int) []time.Duration {
	ages := make([]time.Duration, 0, count)
	for range count {
		ages = append(ages, 48*time.Hour)
	}

	return ages
}

// newDiscardLogger writes nowhere, since no test asserts on the purge's own line.
func newDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeArchive is the archive as rows in memory, an id and when it was put away.
type fakeArchive struct {
	agents []agentstore.Agent
	// reads counts the calls that chose a batch, so a pass that never looked is
	// distinguishable from one that looked and found nothing.
	reads int
	// beforeReread runs between choosing a batch and reading it back, which is where the
	// restore lands in the case about the race.
	beforeReread func()
}

// newFakeArchive builds an archive holding one agent per age given.
func newFakeArchive(ages ...time.Duration) *fakeArchive {
	now := time.Now()

	archive := &fakeArchive{}
	for i, ago := range ages {
		archivedAt := now.Add(-ago)
		archive.agents = append(archive.agents, agentstore.Agent{
			ID:         buildAgentID(i),
			ArchivedAt: &archivedAt,
		})
	}

	return archive
}

func (f *fakeArchive) ListArchivedBefore(_ context.Context, cutoff time.Time, limit int32) ([]string, error) {
	f.reads++

	ids := make([]string, 0, limit)
	for _, agent := range f.agents {
		if agent.ArchivedAt == nil || !agent.ArchivedAt.Before(cutoff) {
			continue
		}
		if len(ids) == int(limit) {
			break
		}

		ids = append(ids, agent.ID)
	}

	return ids, nil
}

func (f *fakeArchive) ListByID(_ context.Context, ids []string) ([]agentstore.Agent, error) {
	if f.beforeReread != nil {
		reread := f.beforeReread
		f.beforeReread = nil
		reread()
	}

	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}

	found := make([]agentstore.Agent, 0, len(ids))
	for _, agent := range f.agents {
		if wanted[agent.ID] {
			found = append(found, agent)
		}
	}

	return found, nil
}

// ids is every agent the archive holds, in the order it holds them.
func (f *fakeArchive) ids() []string {
	ids := make([]string, 0, len(f.agents))
	for _, agent := range f.agents {
		ids = append(ids, agent.ID)
	}

	return ids
}

// restore clears an agent's archive time.
func (f *fakeArchive) restore(id string) {
	for i := range f.agents {
		if f.agents[i].ID == id {
			f.agents[i].ArchivedAt = nil
		}
	}
}

// remove takes agents out of the archive, as the delete would have.
func (f *fakeArchive) remove(ids []string) {
	gone := make(map[string]bool, len(ids))
	for _, id := range ids {
		gone[id] = true
	}

	kept := make([]agentstore.Agent, 0, len(f.agents))
	for _, agent := range f.agents {
		if !gone[agent.ID] {
			kept = append(kept, agent)
		}
	}

	f.agents = kept
}

// fakePurger stands in for the agent service's delete, recording what it was handed and
// taking those rows out so the worker's loop terminates as it would against a database.
type fakePurger struct {
	archive *fakeArchive
	deleted []string
	err     error
}

func (f *fakePurger) DeleteAgents(_ context.Context, ids []string) error {
	if f.err != nil {
		return f.err
	}

	f.deleted = append(f.deleted, ids...)
	f.archive.remove(ids)

	return nil
}

// fakeTables counts the vacuums, since a pass that deleted nothing must not take the locks.
type fakeTables struct {
	vacuums int
}

func (f *fakeTables) TotalRelationSize(context.Context, ...string) (int64, error) {
	return 0, nil
}

func (f *fakeTables) Vacuum(context.Context, ...string) error {
	f.vacuums++

	return nil
}

// buildAgentID is a stable id per seeded agent. Nothing here parses one.
func buildAgentID(i int) string {
	return fmt.Sprintf("agent-%d", i)
}
