package internaljobs

import (
	"context"
	"errors"
	"fmt"
	"testing"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/storetest"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmptyArchiveSweepsEveryPage covers the walk by cursor.
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
			st := storetest.Open(t)
			workflow := st.SeedWorkflow(t, workflowstore.Config{Name: "backend"})
			for i := range tt.archived {
				seedArchived(t, st, agentstore.Agent{Name: fmt.Sprintf("agent-%d", i), WorkflowID: workflow.ID})
			}

			deleter := &recordingDeleter{agents: st.Agents, failOn: tt.failOn, keepRows: tt.keepRows}
			worker := NewEmptyArchiveWorker(st.Agents, deleter)

			err := worker.Work(t.Context(), &river.Job[EmptyArchiveArgs]{Args: EmptyArchiveArgs{}})

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantDeleted, deleter.deleted, "how many archived agents the sweep deleted")
		})
	}
}

type recordingDeleter struct {
	agents  *agentstore.AgentStore
	deleted int
	pages   int

	// asked is the ids each delete was given, which says what the sweep left out of a page as well
	// as what it deleted.
	asked [][]string

	// failOn is the page, counted from one, whose delete refuses, and zero is every page
	// going through.
	failOn int

	// keepRows is the delete succeeding while leaving the rows where they are.
	keepRows bool
}

func (d *recordingDeleter) DeleteAgents(ctx context.Context, ids []string) error {
	d.pages++
	d.asked = append(d.asked, ids)

	if d.pages == d.failOn {
		return errors.New("the delete would not go")
	}

	d.deleted += len(ids)

	if d.keepRows {
		return nil
	}

	_, err := d.agents.Delete(ctx, ids)

	return err
}

func seedArchived(t *testing.T, st *storetest.Stores, agent agentstore.Agent) *agentstore.Agent {
	t.Helper()

	seeded := st.SeedAgent(t, agent)
	_, err := st.Agents.Archive(t.Context(), []string{seeded.ID})
	require.NoError(t, err)

	return seeded
}

func requireArchiveEmpty(t *testing.T, st *storetest.Stores) {
	t.Helper()

	left, err := st.Agents.ListArchiveSweepPage(t.Context(), emptyArchiveBatch, nil)
	require.NoError(t, err)
	assert.Empty(t, left)
}

// TestEmptyArchiveAsksForTheParents pins that a page holding a child whose parent is in the
// archive too names only that parent, the delete taking the child with it.
func TestEmptyArchiveAsksForTheParents(t *testing.T) {
	st := storetest.Open(t)
	parent := st.SeedAgent(t, agentstore.Agent{Name: "author"})
	seedArchived(t, st, agentstore.Agent{Name: "reviewer", WorkflowID: parent.WorkflowID, ParentAgentID: &parent.ID})
	_, err := st.Agents.Archive(t.Context(), []string{parent.ID})
	require.NoError(t, err)

	deleter := &recordingDeleter{agents: st.Agents}
	worker := NewEmptyArchiveWorker(st.Agents, deleter)

	require.NoError(t, worker.Work(t.Context(), &river.Job[EmptyArchiveArgs]{Args: EmptyArchiveArgs{}}))

	assert.Equal(t, [][]string{{parent.ID}}, deleter.asked)
	requireArchiveEmpty(t, st)
}

// TestEmptyArchiveTakesAChildWhoseParentIsLive pins the row nothing else can reach: a child
// archived on its own, whose parent is still working and is never named by the sweep.
func TestEmptyArchiveTakesAChildWhoseParentIsLive(t *testing.T) {
	st := storetest.Open(t)
	parent := st.SeedAgent(t, agentstore.Agent{Name: "author"})
	child := seedArchived(t, st, agentstore.Agent{Name: "reviewer", WorkflowID: parent.WorkflowID, ParentAgentID: &parent.ID})

	deleter := &recordingDeleter{agents: st.Agents}
	worker := NewEmptyArchiveWorker(st.Agents, deleter)

	require.NoError(t, worker.Work(t.Context(), &river.Job[EmptyArchiveArgs]{Args: EmptyArchiveArgs{}}))

	assert.Equal(t, [][]string{{child.ID}}, deleter.asked)
	requireArchiveEmpty(t, st)
}
