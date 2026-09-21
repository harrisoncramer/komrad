package jobs

import (
	"context"

	"weave/internal/jobs/internaljobs"
	agentstore "weave/internal/store/agent"

	"github.com/jackc/pgx/v5"
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
	return s.queue.InsertTx(ctx, internaljobs.EmptyArchiveArgs{}, func(tx pgx.Tx) error {
		return agentstore.NewAgentStore(tx).MarkArchivedDeleting(ctx)
	})
}
