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

// emptyArchiveBatch is how many archived agents one delete covers. Deleting the archive in
// batches keeps one vanished agent from taking the whole sweep with it, since a delete
// refuses the batch it is given whole.
const emptyArchiveBatch = 25

type EmptyArchiveArgs struct{}

// Kind is the River job kind.
func (EmptyArchiveArgs) Kind() string { return "empty_archive" }

// InsertOpts keeps one sweep in flight. The arguments are empty, so uniqueness by them is
// uniqueness of the job itself, and asking again while a sweep is queued, running, or waiting
// on a retry collapses into the sweep already under way.
//
// The states are River's defaults without completed. A finished sweep says nothing about the
// archive now, which fills up again as agents are put away, so leaving completed in would
// swallow every later press for as long as River keeps the row.
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
// deletes agents.
func NewEmptyArchiveWorker(archived ArchivedAgentLister, deleter AgentDeleter) *EmptyArchiveWorker {
	return &EmptyArchiveWorker{archived: archived, agents: deleter}
}

// Work deletes the archive a page at a time, walking it by cursor rather than re-reading the
// front of it, so a page that will not go does not stop the sweep reaching the rest. A batch
// that fails is gathered and returned at the end, which is what puts the sweep in line for a
// retry, and a retry reads the archive fresh rather than carrying ids in the job so it asks
// only for what is still there.
//
// An agent whose cleanup action is still running, or whose cleanup failed, is left behind by
// the delete rather than failing it, so a sweep can finish with rows in the archive and no
// error to show for it.
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

		ids := xslice.Map(batch, func(agent agentstore.Agent) string { return agent.ID })
		if err := w.agents.DeleteAgents(ctx, ids); err != nil {
			failures = append(failures, fmt.Errorf("failed to delete %d archived agents: %w", len(ids), err))
		}
	}
}
