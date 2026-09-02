package internaljobs

import (
	"context"
	"errors"
	"fmt"

	agentstore "weave/internal/store/agent"
	"weave/lib/xslice"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// emptyArchiveBatch is how many archived agents one delete covers, in batches so one
// vanished agent does not take the whole sweep with it.
const emptyArchiveBatch = 25

type EmptyArchiveArgs struct{}

// Kind is the River job kind.
func (EmptyArchiveArgs) Kind() string { return "empty_archive" }

// InsertOpts keeps one sweep in flight, using River's default states without completed so a
// finished sweep does not swallow every later press.
func (EmptyArchiveArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		UniqueOpts: river.UniqueOpts{
			ByArgs: true,
			ByState: []rivertype.JobState{
				rivertype.JobStateAvailable,
				rivertype.JobStatePending,
				rivertype.JobStateRetryable,
				rivertype.JobStateRunning,
				rivertype.JobStateScheduled,
			},
		},
	}
}

type EmptyArchiveWorker struct {
	river.WorkerDefaults[EmptyArchiveArgs]
	archived ArchivedAgentLister
	agents   AgentDeleter
}

// NewEmptyArchiveWorker builds the worker over the archive it reads and the service that
// deletes agents, which also deletes a workflow left with nothing in it.
func NewEmptyArchiveWorker(archived ArchivedAgentLister, deleter AgentDeleter) *EmptyArchiveWorker {
	return &EmptyArchiveWorker{archived: archived, agents: deleter}
}

// Work deletes the archive a page at a time by cursor, so a page that will not go does not
// stop the sweep reaching the rest.
func (w *EmptyArchiveWorker) Work(ctx context.Context, job *river.Job[EmptyArchiveArgs]) error {
	var (
		failures []error
		cursor   *agentstore.Cursor
	)

	for {
		batch, err := w.archived.ListPage(ctx, emptyArchiveBatch, cursor, true)
		if err != nil {
			failures = append(failures, fmt.Errorf("failed to read the archive: %w", err))

			return errors.Join(failures...)
		}
		if len(batch) == 0 {
			return errors.Join(failures...)
		}

		cursor = &agentstore.Cursor{ID: batch[len(batch)-1].ID}

		// A child is deleted by naming its parent, and naming one on its own is refused, so the
		// page asks for the parents and the children on it go with whichever page holds theirs.
		parents := xslice.Filter(batch, func(agent agentstore.Agent) bool { return agent.ParentAgentID == nil })
		if len(parents) == 0 {
			continue
		}

		// An agent whose cleanup is still running or failed is left behind rather than
		// failing the batch, so a sweep can finish with rows still in the archive.
		ids := xslice.Map(parents, func(agent agentstore.Agent) string { return agent.ID })
		if err := w.agents.DeleteAgents(ctx, ids); err != nil {
			failures = append(failures, fmt.Errorf("failed to delete %d archived agents: %w", len(ids), err))
		}
	}
}
