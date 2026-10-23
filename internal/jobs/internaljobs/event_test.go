package internaljobs

import (
	"context"
	"errors"
	"testing"

	"komrad/internal/events"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	mailstore "komrad/internal/store/mail"
	"komrad/internal/storetest"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingRouter struct {
	recipient uuid.UUID
}

func (r failingRouter) Route(ctx context.Context, tx store.DBTX, _ events.Event) ([]river.JobArgs, error) {
	_, err := mailstore.NewMailStore(tx).UpsertFromKomrad(ctx, mailstore.UpsertFromKomradParams{
		FromName:  "komrad",
		ToAgentID: r.recipient,
		Body:      "written before the handler failed",
		Kind:      agentv1.MailKind_MAIL_KIND_PULL_REQUEST_CHANGED,
		SubjectID: uuid.New(),
	})
	if err != nil {
		return nil, err
	}

	return nil, errors.New("the handler failed after writing")
}

// TestEventWorkerRollsBackAFailedHandler pins that a handler failing part way leaves no mail behind and its job retries.
func TestEventWorkerRollsBackAFailedHandler(t *testing.T) {
	stores := storetest.Open(t)
	agent := stores.SeedAgent(t, agentstore.Agent{
		ID:   uuid.New(),
		Name: "a",
		Cwd:  "/repo",
	})
	worker := NewEventWorker(stores.Pool.GetRawPool(), failingRouter{recipient: agent.ID})

	err := worker.Work(t.Context(), &river.Job[events.Event]{
		JobRow: &rivertype.JobRow{Attempt: 1, MaxAttempts: 3},
		Args:   events.Event{Topic: "pull_request_changed"},
	})

	require.Error(t, err, "an error hands the job back to river to retry")
	held, err := stores.Mail.ListPage(t.Context(), agent.ID, 10, nil)
	require.NoError(t, err)
	assert.Empty(t, held)
}
