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
	releaser DeletionReleaser
}

type NewEmptyArchiveWorkerParams struct {
	Archived ArchivedAgentLister
	Deleter  AgentDeleter
	Releaser DeletionReleaser
}

// NewEmptyArchiveWorker builds the worker over the archive it reads, the service that deletes
// agents, and the store that lets go of a row the delete could not take.
func NewEmptyArchiveWorker(params NewEmptyArchiveWorkerParams) *EmptyArchiveWorker {
	return &EmptyArchiveWorker{
		archived: params.Archived,
		agents:   params.Deleter,
		releaser: params.Releaser,
	}
}

// Work deletes the archive a page at a time by cursor, so a page that will not go does not
// stop the sweep reaching the rest.
func (w *EmptyArchiveWorker) Work(ctx context.Context, job *river.Job[EmptyArchiveArgs]) error {
	var (
		failures []error
		cursor   *agentstore.Cursor
	)

	for {
		batch, err := w.archived.ListArchiveSweepPage(ctx, emptyArchiveBatch, cursor)
		if err != nil {
			failures = append(failures, fmt.Errorf("failed to read the archive: %w", err))

			return errors.Join(failures...)
		}
		if len(batch) == 0 {
			return errors.Join(failures...)
		}

		cursor = &agentstore.Cursor{ID: batch[len(batch)-1].ID}

		// A child whose parent is in the archive too goes with whichever page holds that
		// parent, so this page asks for the parents and for the children nothing else reaches.
		named := xslice.Filter(batch, func(agent agentstore.Agent) bool { return agent.ParentAgentID == nil })

		orphaned, err := w.orphanedChildren(ctx, batch)
		if err != nil {
			failures = append(failures, err)
		}
		named = append(named, orphaned...)

		if len(named) == 0 {
			continue
		}

		// An agent whose cleanup is still running or failed is left behind rather than
		// failing the batch, so a sweep can finish with rows still in the archive.
		ids := xslice.Map(named, func(agent agentstore.Agent) string { return agent.ID })
		if err := w.agents.DeleteAgents(ctx, ids); err != nil {
			failures = append(failures, fmt.Errorf("failed to delete %d archived agents: %w", len(ids), err))
			failures = append(failures, releaseClaims(ctx, w.releaser, ids)...)
		}
	}
}

// releaseClaims lets the rows of a delete that did not start back into the listings.
func releaseClaims(ctx context.Context, releaser DeletionReleaser, ids []string) []error {
	if err := releaser.ClearDeleting(ctx, ids); err != nil {
		return []error{fmt.Errorf("failed to release %d agents the delete could not take: %w", len(ids), err)}
	}

	return nil
}

// orphanedChildren picks the children on a page whose parent is not in the archive with them,
// which nothing else would reach.
func (w *EmptyArchiveWorker) orphanedChildren(ctx context.Context, batch []agentstore.Agent) ([]agentstore.Agent, error) {
	children := xslice.Filter(batch, func(agent agentstore.Agent) bool { return agent.ParentAgentID != nil })
	if len(children) == 0 {
		return nil, nil
	}

	parents, err := w.archived.ListByID(ctx, xslice.Map(children, func(agent agentstore.Agent) string { return *agent.ParentAgentID }))
	if err != nil {
		return nil, fmt.Errorf("failed to look up the parents of %d archived children: %w", len(children), err)
	}

	inArchive := make(map[string]bool, len(parents))
	for _, parent := range parents {
		inArchive[parent.ID] = parent.ArchivedAt != nil
	}

	return xslice.Filter(children, func(child agentstore.Agent) bool { return !inArchive[*child.ParentAgentID] }), nil
}
