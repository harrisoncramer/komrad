package internaljobs

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"time"

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

	// live is the agents outside the archive, which the sweep never pages through but does ask
	// about when a child on a page names one as its parent.
	live []agentstore.Agent

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
		archive.agents = append(archive.agents, archivedRow(agentstore.Agent{ID: fmt.Sprintf("%06d", i)}))
	}

	return archive
}

// archivedRow stamps the archive time the store carries on a row the sweep reads, which is what
// tells an archived parent from a live one.
func archivedRow(agent agentstore.Agent) agentstore.Agent {
	agent.ArchivedAt = xptr.Ptr(time.Now())

	return agent
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

// ListByID spans the archive and the live agents beside it, the way the store's own read does,
// since what the sweep asks it is whether a parent is archived rather than whether it exists.
func (f *fakeArchive) ListByID(_ context.Context, ids []string) ([]agentstore.Agent, error) {
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}

	var found []agentstore.Agent
	for _, agent := range append(append([]agentstore.Agent{}, f.agents...), f.live...) {
		if wanted[agent.ID] {
			found = append(found, agent)
		}
	}

	return found, nil
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

// TestEmptyArchiveAsksForTheParents pins that a page holding a child whose parent is in the
// archive too names only that parent, since the delete takes the child with it.
func TestEmptyArchiveAsksForTheParents(t *testing.T) {
	archive := newFakeArchive(0)
	archive.agents = []agentstore.Agent{
		archivedRow(agentstore.Agent{ID: "000002", ParentAgentID: xptr.Ptr("000001")}),
		archivedRow(agentstore.Agent{ID: "000001"}),
	}
	worker := NewEmptyArchiveWorker(archive, archive)

	require.NoError(t, worker.Work(t.Context(), &river.Job[EmptyArchiveArgs]{Args: EmptyArchiveArgs{}}))

	assert.Equal(t, [][]string{{"000001"}}, archive.asked)
	assert.Empty(t, archive.agents, "and the child goes with it")
}

// TestEmptyArchiveTakesAChildWhoseParentIsLive pins the row nothing else can reach: a child
// archived on its own, whose parent is still working and is never named by the sweep.
func TestEmptyArchiveTakesAChildWhoseParentIsLive(t *testing.T) {
	archive := newFakeArchive(0)
	archive.agents = []agentstore.Agent{archivedRow(agentstore.Agent{ID: "000002", ParentAgentID: xptr.Ptr("000001")})}
	archive.live = []agentstore.Agent{{ID: "000001"}}
	worker := NewEmptyArchiveWorker(archive, archive)

	require.NoError(t, worker.Work(t.Context(), &river.Job[EmptyArchiveArgs]{Args: EmptyArchiveArgs{}}))

	assert.Equal(t, [][]string{{"000002"}}, archive.asked)
	assert.Empty(t, archive.agents, "the archive is left empty")
}
