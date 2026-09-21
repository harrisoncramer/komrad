package jobs

import (
	"context"

	"weave/internal/jobs/internaljobs"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
)

type ArchiveSweep struct {
	queue *Enqueuer
}

// NewArchiveSweep builds the queuer of the sweep that empties the archive.
func NewArchiveSweep(queue *Enqueuer) *ArchiveSweep {
	return &ArchiveSweep{queue: queue}
}

// EmptyArchive claims every archived agent, and the archived workflows that leaves nothing
// unclaimed in, and queues the sweep that deletes them in the same transaction.
func (s *ArchiveSweep) EmptyArchive(ctx context.Context) error {
	return s.queue.InsertTx(ctx, internaljobs.EmptyArchiveArgs{}, func(tx store.DBTX) error {
		return agentstore.NewAgentStore(tx).MarkArchivedDeleting(ctx)
	})
}
