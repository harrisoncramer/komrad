package jobs

import (
	"context"
	"errors"
	"fmt"
	"testing"

	agentstore "weave/internal/store/agent"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmptyArchiveSweepsEveryPage covers the walk. The archive is deleted a page at a time
// and a page is read by cursor, so a sweep that asked the front of the archive twice, or
// stopped at the first page, would leave agents behind and report nothing wrong.
func TestEmptyArchiveSweepsEveryPage(t *testing.T) {
	tests := []struct {
		name string

		archived int
		failOn   int

		wantDeleted int
		wantErr     bool
	}{
		{
			name:        "an archive smaller than a page",
			archived:    3,
			wantDeleted: 3,
		},
		{
			name:        "an archive of several pages",
			archived:    emptyArchiveBatch*2 + 4,
			wantDeleted: emptyArchiveBatch*2 + 4,
		},
		{
			name:     "an empty archive",
			archived: 0,
		},
		{
			name:        "a page that will not go leaves the rest of the sweep alone",
			archived:    emptyArchiveBatch * 3,
			failOn:      2,
			wantDeleted: emptyArchiveBatch * 2,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			archive := newFakeArchive(tt.archived)
			archive.failOn = tt.failOn
			worker := NewEmptyArchiveWorker(archive, archive)

			err := worker.Work(t.Context(), &river.Job[EmptyArchiveArgs]{Args: EmptyArchiveArgs{}})

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantDeleted, archive.deleted, "how many archived agents the sweep deleted")
		})
	}
}

// TestEmptyArchiveIsQueuedOnce covers River's side of it. The sweep takes as long as tearing
// down that many agents takes, so a second press while one is under way has to collapse into
// it rather than starting a second sweep over the same archive.
func TestEmptyArchiveIsQueuedOnce(t *testing.T) {
	client := newRiverClient(t)

	first, err := client.Insert(t.Context(), EmptyArchiveArgs{}, nil)
	require.NoError(t, err)
	require.False(t, first.UniqueSkippedAsDuplicate)

	second, err := client.Insert(t.Context(), EmptyArchiveArgs{}, nil)
	require.NoError(t, err)

	assert.True(t, second.UniqueSkippedAsDuplicate, "a sweep already in flight is the one that runs")
}

type fakeArchive struct {
	agents []agentstore.Agent

	deleted int
	pages   int

	// failOn is the page, counted from one, whose delete refuses. Zero is every page going
	// through, which is what the zero value of this fake has to mean for the callers that
	// only need an archive with nothing in it.
	failOn int
}

func newFakeArchive(count int) *fakeArchive {
	archive := &fakeArchive{}
	for i := count; i > 0; i-- {
		archive.agents = append(archive.agents, agentstore.Agent{ID: fmt.Sprintf("%06d", i)})
	}

	return archive
}

func (f *fakeArchive) ListPage(_ context.Context, limit int64, cursor *agentstore.Cursor, archived bool) ([]agentstore.Agent, error) {
	if !archived {
		return nil, errors.New("the sweep reads the archive, not the live agents")
	}

	page := make([]agentstore.Agent, 0, limit)
	for _, agent := range f.agents {
		if cursor != nil && agent.ID >= cursor.ID {
			continue
		}
		if int64(len(page)) == limit {
			break
		}

		page = append(page, agent)
	}

	return page, nil
}

func (f *fakeArchive) DeleteAgents(_ context.Context, ids []string) error {
	f.pages++

	if f.pages == f.failOn {
		return errors.New("the delete would not go")
	}

	gone := make(map[string]bool, len(ids))
	for _, id := range ids {
		gone[id] = true
	}

	kept := f.agents[:0]
	for _, agent := range f.agents {
		if !gone[agent.ID] {
			kept = append(kept, agent)
		}
	}
	f.agents = kept
	f.deleted += len(ids)

	return nil
}
