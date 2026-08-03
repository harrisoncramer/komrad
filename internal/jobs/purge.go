package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"weave/internal/logging"

	"github.com/riverqueue/river"
)

// purgeBatchSize is how many agents one delete takes. Each row cascades to its transcript,
// its search chunks, and its metadata.
const purgeBatchSize = 100

// purgeTimeout bounds one pass, since the queue's own timeout is sized for a launch. The
// first pass after a horizon is set walks an archive nothing has ever left.
const purgeTimeout = 10 * time.Minute

// purgedTables are the tables a purge empties and then vacuums. Three of them are reached
// through the cascade rather than named by the delete.
var purgedTables = []string{"agents", "agent_transcripts", "agent_transcript_chunks", "agent_metadata"}

// PurgeArchiveArgs is one pass over the archive. The horizon travels as a duration rather
// than a computed cutoff, so two inserts for the same purge carry identical arguments and
// collapse on the unique index below.
type PurgeArchiveArgs struct {
	After time.Duration `json:"after"`
}

// Kind is the River job kind.
func (PurgeArchiveArgs) Kind() string { return "purge_archive" }

// InsertOpts dedups a pass by its arguments, so the next tick does not queue a second one
// beside a purge already running. The states are River's defaults rather than a launch's,
// since a purge that cancelled has to be free to run again.
func (PurgeArchiveArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// PurgeArchiveWorker deletes the agents that have sat in the archive past the horizon and
// makes the space they held reusable.
type PurgeArchiveWorker struct {
	river.WorkerDefaults[PurgeArchiveArgs]
	archive ArchiveReader
	agents  AgentPurger
	tables  TableMaintainer
	logger  *slog.Logger
}

// PurgeArchiveParams is what the purge is built from.
type PurgeArchiveParams struct {
	Archive ArchiveReader
	Purger  AgentPurger
	Tables  TableMaintainer
	Logger  *slog.Logger
}

// NewPurgeArchiveWorker builds the worker that empties the archive.
func NewPurgeArchiveWorker(p PurgeArchiveParams) *PurgeArchiveWorker {
	return &PurgeArchiveWorker{archive: p.Archive, agents: p.Purger, tables: p.Tables, logger: p.Logger}
}

// Timeout gives a purge its own budget rather than the queue's, which is set for a launch.
func (w *PurgeArchiveWorker) Timeout(*river.Job[PurgeArchiveArgs]) time.Duration {
	return purgeTimeout
}

// Work deletes what the horizon has caught up with. The cutoff is worked out here rather
// than at insert, so a job that waited on the queue measures from when it ran, and a pass
// that removed nothing skips the vacuum.
func (w *PurgeArchiveWorker) Work(ctx context.Context, job *river.Job[PurgeArchiveArgs]) error {
	// Zero is never. No periodic job is registered for it, so this is a pass queued before
	// the setting was cleared.
	if job.Args.After <= 0 {
		return nil
	}

	cutoff := time.Now().Add(-job.Args.After)

	before, err := w.tables.TotalRelationSize(ctx, purgedTables...)
	if err != nil {
		return err
	}

	// A pass that failed partway still vacuums and reports what it took, since those rows
	// are gone either way.
	removed, purgeErr := w.purge(ctx, cutoff)
	if removed == 0 {
		return purgeErr
	}

	if err := w.tables.Vacuum(ctx, purgedTables...); err != nil {
		return errors.Join(purgeErr, err)
	}

	after, err := w.tables.TotalRelationSize(ctx, purgedTables...)
	if err != nil {
		return errors.Join(purgeErr, err)
	}

	w.logger.LogAttrs(ctx, slog.LevelInfo, "purged the archive",
		slog.Int(logging.PurgedAgents, removed),
		slog.String(logging.PurgeCutoff, cutoff.Format(time.RFC3339)),
		slog.Int64(logging.BytesBefore, before),
		slog.Int64(logging.BytesAfter, after))

	return purgeErr
}

// purge deletes the agents archived before cutoff in bounded batches and reports how many
// went. It batches so no one transaction holds a snapshot over the whole cascade.
func (w *PurgeArchiveWorker) purge(ctx context.Context, cutoff time.Time) (int, error) {
	removed := 0

	for {
		ids, err := w.archive.ListArchivedBefore(ctx, cutoff, purgeBatchSize)
		if err != nil {
			return removed, err
		}
		if len(ids) == 0 {
			return removed, nil
		}

		purgeable, err := w.keepStillArchived(ctx, ids, cutoff)
		if err != nil {
			return removed, err
		}
		// The whole batch was restored under this pass. Reading the same window again is
		// how a pass could go round forever, so the rest keeps until the next one.
		if len(purgeable) == 0 {
			return removed, nil
		}

		if err := w.agents.DeleteAgents(ctx, purgeable); err != nil {
			return removed, fmt.Errorf("failed to purge %d archived agents: %w", len(purgeable), err)
		}

		removed += len(purgeable)
	}
}

// keepStillArchived narrows a batch to the agents still archived past the cutoff, since a
// restore can land between choosing the batch and deleting it and the delete is keyed on ids
// alone. It narrows that window rather than closing it, which would take a delete carrying
// the predicate and so a second way out of the archive that kills no pane.
func (w *PurgeArchiveWorker) keepStillArchived(ctx context.Context, ids []string, cutoff time.Time) ([]string, error) {
	agents, err := w.archive.ListByID(ctx, ids)
	if err != nil {
		return nil, err
	}

	purgeable := make([]string, 0, len(agents))
	for _, agent := range agents {
		if agent.ArchivedAt != nil && agent.ArchivedAt.Before(cutoff) {
			purgeable = append(purgeable, agent.ID)
		}
	}

	return purgeable, nil
}
