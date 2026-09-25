package agent

import (
	"context"
	"testing"
	"time"

	"komrade/internal/dbtest"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLockSlots pins which slot claims wait on one another.
func TestLockSlots(t *testing.T) {
	database := dbtest.New(t)
	engine := dbtest.NewEngine(t, database)
	otherEngine := dbtest.NewEngine(t, database)

	tests := []struct {
		name        string
		holderURL   string
		claimantURL string
		wantWait    bool
	}{
		{
			name:        "a second claim by the same engine waits for the first",
			holderURL:   engine.URL,
			claimantURL: engine.URL,
			wantWait:    true,
		},
		{
			name:        "two logins that belong to no engine still wait on each other",
			holderURL:   database.OwnerURL,
			claimantURL: database.OwnerURL,
			wantWait:    true,
		},
		{
			name:        "an engine does not wait on a login that belongs to no engine",
			holderURL:   database.OwnerURL,
			claimantURL: engine.URL,
		},
		{
			name:        "another engine does not wait",
			holderURL:   engine.URL,
			claimantURL: otherEngine.URL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			holder := beginTx(t, tt.holderURL)
			require.NoError(t, NewAgentStore(holder).LockSlots(t.Context()))

			claimant := beginTx(t, tt.claimantURL)
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()

			err := NewAgentStore(claimant).LockSlots(ctx)
			if tt.wantWait {
				assert.ErrorIs(t, err, context.DeadlineExceeded)
				return
			}

			assert.NoError(t, err)
		})
	}
}

func beginTx(t *testing.T, url string) pgx.Tx {
	t.Helper()

	conn, err := pgx.Connect(t.Context(), url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(t.Context()) })

	tx, err := conn.Begin(t.Context())
	require.NoError(t, err)

	return tx
}
