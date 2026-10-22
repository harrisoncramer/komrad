package forge

import (
	"testing"
	"time"

	"komrad/internal/dbtest"
	"komrad/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func newStore(t *testing.T) *ForgeStore {
	t.Helper()

	pool, err := store.Open(t.Context(), dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	return NewForgeStore(pool.GetDB())
}

func github(verifiedAt time.Time) CreateParams {
	return CreateParams{
		Kind:       "github",
		Host:       "github.com",
		Method:     "token",
		Login:      "harrisoncramer",
		VerifiedAt: verifiedAt,
	}
}

// TestCreateRefusesASecondConnectionForAHost covers the one identity per host rule.
func TestCreateRefusesASecondConnectionForAHost(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()

	_, err := st.Create(ctx, github(time.Now()))
	require.NoError(t, err)

	_, err = st.Create(ctx, github(time.Now()))
	require.ErrorIs(t, err, ErrHostTaken)
}

// TestNotFound covers every lookup and write against an id or host nobody holds.
func TestNotFound(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	var missing = uuid.MustParse("00000000-0000-0000-0000-000000000000")

	_, err := st.Get(ctx, missing)
	require.ErrorIs(t, err, store.ErrNotFound)

	_, err = st.GetByHost(ctx, "nowhere.example.com")
	require.ErrorIs(t, err, store.ErrNotFound)

	require.ErrorIs(t, st.SetVerifiedAt(ctx, missing, nil), store.ErrNotFound)
	require.ErrorIs(t, st.Delete(ctx, missing), store.ErrNotFound)
}
