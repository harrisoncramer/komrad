package internaljobs

import (
	"context"
	"errors"
	"fmt"
	"testing"

	agentstore "weave/internal/store/agent"
	"weave/lib/xptr"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmptyArchiveSweepsEveryPage covers the walk by cursor, since asking the front of the
// archive twice or stopping at the first page would leave agents behind and say nothing.
func TestEmptyArchiveSweepsEveryPage(t *testing.T) {
	tests := []struct {
		name        string
		archived    int
		failOn      int
		keepRows    bool
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
			name:        "a page whose cleanup has not finished stays put without stalling the sweep",
			archived:    emptyArchiveBatch*2 + 4,
			keepRows:    true,
			wantDeleted: emptyArchiveBatch*2 + 4,
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
			archive.keepRows = tt.keepRows
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

type fakeArchive struct {
	agents  []agentstore.Agent
	deleted int
	pages   int

	// asked is the ids each delete was given, since what the sweep leaves out of a page matters
	// as much as what it deletes.
	asked [][]string

	// failOn is the page, counted from one, whose delete refuses, and zero is every page
	// going through.
	failOn int

	// keepRows is the delete succeeding while leaving the rows where they are, which is what
	// the real one does for an agent whose cleanup action is still running.
	keepRows bool
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
	f.asked = append(f.asked, ids)

	if f.pages == f.failOn {
		return errors.New("the delete would not go")
	}

	f.deleted += len(ids)

	if f.keepRows {
		return nil
	}

	gone := make(map[string]bool, len(ids))
	for _, id := range ids {
		gone[id] = true
	}
	// A child goes with its parent, the way the service it stands in for takes them.
	for _, agent := range f.agents {
		if agent.ParentAgentID != nil && gone[*agent.ParentAgentID] {
			gone[agent.ID] = true
		}
	}

	kept := f.agents[:0]
	for _, agent := range f.agents {
		if !gone[agent.ID] {
			kept = append(kept, agent)
		}
	}
	f.agents = kept

	return nil
}

// TestEmptyArchiveAsksForTheParents pins that a page holding a child names only its parent,
// since a child is deleted by naming the agent it is attached to and naming one alone is
// refused.
func TestEmptyArchiveAsksForTheParents(t *testing.T) {
	archive := newFakeArchive(0)
	archive.agents = []agentstore.Agent{
		{ID: "000002", ParentAgentID: xptr.Ptr("000001")},
		{ID: "000001"},
	}
	worker := NewEmptyArchiveWorker(archive, archive)

	require.NoError(t, worker.Work(t.Context(), &river.Job[EmptyArchiveArgs]{Args: EmptyArchiveArgs{}}))

	assert.Equal(t, [][]string{{"000001"}}, archive.asked)
	assert.Empty(t, archive.agents, "and the child goes with it")
}
