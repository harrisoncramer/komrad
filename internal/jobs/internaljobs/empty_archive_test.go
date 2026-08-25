package internaljobs

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
		keepRows bool

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
			worker := NewEmptyArchiveWorker(archive, archive, &fakeWorkflowDeleter{})

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

// TestEmptyArchiveTellsTheWorkflowsItTouched covers the other half of a sweep: it does not
// decide for itself whether a workflow is now empty, it hands every workflow a deleted batch
// touched to what does, once per distinct id rather than once per agent.
func TestEmptyArchiveTellsTheWorkflowsItTouched(t *testing.T) {
	archive := newFakeArchive(0)
	archive.agents = []agentstore.Agent{
		{ID: "1", WorkflowID: "w1"},
		{ID: "2", WorkflowID: "w1"},
		{ID: "3", WorkflowID: "w2"},
	}
	workflows := &fakeWorkflowDeleter{}
	worker := NewEmptyArchiveWorker(archive, archive, workflows)

	err := worker.Work(t.Context(), &river.Job[EmptyArchiveArgs]{Args: EmptyArchiveArgs{}})
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"w1", "w2"}, workflows.checked)
}

// TestEmptyArchiveKeepsGoingWhenAWorkflowWontDelete covers a workflow delete failing, a
// schedule still pointing at it for instance, the same way a page that will not delete does:
// the failure is reported rather than swallowed, but it does not stop the rest of the sweep.
func TestEmptyArchiveKeepsGoingWhenAWorkflowWontDelete(t *testing.T) {
	archive := newFakeArchive(0)
	archive.agents = []agentstore.Agent{{ID: "1", WorkflowID: "w1"}}
	workflows := &fakeWorkflowDeleter{err: errors.New("a schedule still points at it")}
	worker := NewEmptyArchiveWorker(archive, archive, workflows)

	err := worker.Work(t.Context(), &river.Job[EmptyArchiveArgs]{Args: EmptyArchiveArgs{}})

	require.Error(t, err)
	assert.Equal(t, 1, archive.deleted, "the agent delete still went through")
}

type fakeWorkflowDeleter struct {
	checked []string
	err     error
}

func (f *fakeWorkflowDeleter) DeleteIfEmpty(_ context.Context, workflowID string) error {
	f.checked = append(f.checked, workflowID)

	return f.err
}

type fakeArchive struct {
	agents []agentstore.Agent

	deleted int
	pages   int

	// failOn is the page, counted from one, whose delete refuses. Zero is every page going
	// through, which is what the zero value of this fake has to mean for the callers that
	// only need an archive with nothing in it.
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

	kept := f.agents[:0]
	for _, agent := range f.agents {
		if !gone[agent.ID] {
			kept = append(kept, agent)
		}
	}
	f.agents = kept

	return nil
}
