package forge

import (
	"testing"
	"time"

	"weave/internal/dbtest"
	"weave/internal/store"

	"github.com/stretchr/testify/assert"
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

// TestCreateAndRead covers a connection going in and coming back by id, by host and in the
// list, with the login and the verification stamp it was created with.
func TestCreateAndRead(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	verifiedAt := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

	created, err := st.Create(ctx, github(verifiedAt))
	require.NoError(t, err)
	assert.Equal(t, "github.com", created.Host)
	assert.Equal(t, "harrisoncramer", created.Login)
	require.NotNil(t, created.VerifiedAt)
	assert.True(t, created.VerifiedAt.Equal(verifiedAt))

	byID, err := st.Get(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, byID.ID)

	byHost, err := st.GetByHost(ctx, "github.com")
	require.NoError(t, err)
	assert.Equal(t, created.ID, byHost.ID)

	listed, err := st.List(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, created.ID, listed[0].ID)
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

// TestSetVerifiedAt covers a refusal clearing the stamp and a later check restoring it.
func TestSetVerifiedAt(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()

	created, err := st.Create(ctx, github(time.Now()))
	require.NoError(t, err)

	require.NoError(t, st.SetVerifiedAt(ctx, created.ID, nil))
	cleared, err := st.Get(ctx, created.ID)
	require.NoError(t, err)
	assert.Nil(t, cleared.VerifiedAt)

	again := time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC)
	require.NoError(t, st.SetVerifiedAt(ctx, created.ID, &again))
	restored, err := st.Get(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, restored.VerifiedAt)
	assert.True(t, restored.VerifiedAt.Equal(again))
}

// TestNotFound covers every lookup and write against an id or host nobody holds.
func TestNotFound(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	const missing = "00000000-0000-0000-0000-000000000000"

	_, err := st.Get(ctx, missing)
	require.ErrorIs(t, err, store.ErrNotFound)

	_, err = st.Get(ctx, "not-an-id")
	require.ErrorIs(t, err, store.ErrNotFound)

	_, err = st.GetByHost(ctx, "nowhere.example.com")
	require.ErrorIs(t, err, store.ErrNotFound)

	require.ErrorIs(t, st.SetVerifiedAt(ctx, missing, nil), store.ErrNotFound)
	require.ErrorIs(t, st.Delete(ctx, missing), store.ErrNotFound)
}

// TestDelete covers a connection going away and staying away.
func TestDelete(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()

	created, err := st.Create(ctx, github(time.Now()))
	require.NoError(t, err)

	require.NoError(t, st.Delete(ctx, created.ID))
	_, err = st.GetByHost(ctx, "github.com")
	require.ErrorIs(t, err, store.ErrNotFound)
	require.ErrorIs(t, st.Delete(ctx, created.ID), store.ErrNotFound)
}
