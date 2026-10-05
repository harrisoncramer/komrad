package identity_test

import (
	"testing"

	"komrad/db/testmodels"
	"komrad/internal/dbtest"
	"komrad/internal/principalrole"
	"komrad/internal/store"
	identitystore "komrad/internal/store/identity"
	"komrad/internal/storetest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBindings covers one role per engine, the role a binding replaces, and the cascade from an engine.
func TestBindings(t *testing.T) {
	t.Parallel()

	stores := storetest.OpenOwner(t)
	ctx := t.Context()
	second := dbtest.NewEngine(t, stores.Database)

	first, err := stores.Bindings.GetEngineByName(ctx, stores.Engine.Name)
	require.NoError(t, err)
	other, err := stores.Bindings.GetEngineByName(ctx, second.Name)
	require.NoError(t, err)
	_, err = stores.Bindings.GetEngineByName(ctx, "absent")
	require.ErrorIs(t, err, store.ErrNotFound)

	principal, err := stores.Principals.Create(ctx, identitystore.CreateParams{Name: "ci", Kind: identitystore.KindService})
	require.NoError(t, err)

	previous, err := stores.Bindings.Set(ctx, identitystore.SetParams{
		PrincipalID: principal.ID,
		EngineID:    first.ID,
		Role:        principalrole.Viewer,
	})
	require.NoError(t, err)
	assert.Empty(t, previous, "a first binding replaces nothing")

	previous, err = stores.Bindings.Set(ctx, identitystore.SetParams{
		PrincipalID: principal.ID,
		EngineID:    first.ID,
		Role:        principalrole.Operator,
	})
	require.NoError(t, err)
	assert.Equal(t, principalrole.Viewer, previous)

	_, err = stores.Bindings.Set(ctx, identitystore.SetParams{
		PrincipalID: principal.ID,
		EngineID:    other.ID,
		Role:        principalrole.Automation,
	})
	require.NoError(t, err)

	listed, err := stores.Bindings.List(ctx)
	require.NoError(t, err)
	want := []identitystore.Binding{
		{
			PrincipalID: principal.ID,
			EngineID:    first.ID,
			EngineName:  first.Name,
			Role:        principalrole.Operator,
		},
		{
			PrincipalID: principal.ID,
			EngineID:    other.ID,
			EngineName:  other.Name,
			Role:        principalrole.Automation,
		},
	}
	if first.Name > other.Name {
		want[0], want[1] = want[1], want[0]
	}
	assert.Equal(t, want, listed, "one role per engine, ordered by engine name")

	require.NoError(t, stores.Bindings.Delete(ctx, principal.ID, first.ID))
	require.NoError(t, stores.Bindings.Delete(ctx, principal.ID, first.ID), "unbinding what is not bound is not an error")

	testQueries := testmodels.New(stores.Pool)
	require.NoError(t, testQueries.DeleteEngineDocumentTypes(ctx, other.ID))
	require.NoError(t, testQueries.DeleteEngine(ctx, other.ID))

	listed, err = stores.Bindings.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, listed, "deleting an engine takes its bindings with it")
}
