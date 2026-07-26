package session

import (
	"context"
	"testing"
	"time"

	"claude-agents/internal/store"
	sessionv1 "claude-agents/proto/gen/session/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreatedAtIsEmitted checks the creation time is emitted over the RPC as RFC 3339.
func TestCreatedAtIsEmitted(t *testing.T) {
	svc, st := service(t)
	ctx := context.Background()
	require.NoError(t, st.Upsert(ctx, store.Agent{Name: "alpha", State: store.StateNew}))

	got, err := svc.GetSession(ctx, connect.NewRequest(&sessionv1.GetSessionRequest{Name: "alpha"}))
	require.NoError(t, err)

	created := got.Msg.GetSession().GetCreatedAt()
	require.NotEmpty(t, created, "created_at must be emitted")
	ts, err := time.Parse(time.RFC3339, created)
	require.NoError(t, err, "created_at must be RFC 3339")
	assert.WithinDuration(t, time.Now(), ts, time.Minute, "created_at should be recent")
}
