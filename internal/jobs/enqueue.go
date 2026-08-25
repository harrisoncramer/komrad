package jobs

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

type Enqueuer struct {
	client *river.Client[pgx.Tx]
}

// NewEnqueuer wraps the River client for the callers that only queue work.
func NewEnqueuer(client *river.Client[pgx.Tx]) *Enqueuer {
	return &Enqueuer{client: client}
}

// EmptyArchive queues the sweep that deletes every archived agent. It returns once the job
// is recorded, since the sweep runs for as long as tearing down that many agents takes and
// nobody is waiting on it.
func (e *Enqueuer) EmptyArchive(ctx context.Context) error {
	if _, err := e.client.Insert(ctx, EmptyArchiveArgs{}, nil); err != nil {
		return fmt.Errorf("failed to queue emptying the archive: %w", err)
	}

	return nil
}
