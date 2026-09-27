package identity_test

import (
	"testing"

	"komrad/db/testmodels"
	"komrad/internal/principalrole"
	"komrad/internal/store"
	identitystore "komrad/internal/store/identity"
	"komrad/internal/storetest"
	"komrad/lib/xptr"
	"komrad/lib/xslice"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreatePrincipal covers the constraints the schema holds a principal to.
func TestCreatePrincipal(t *testing.T) {
	t.Parallel()

	stores := storetest.OpenOwner(t)
	ctx := t.Context()

	_, err := stores.Principals.Create(ctx, identitystore.CreateParams{
		Name:     "holder",
		Kind:     identitystore.KindPerson,
		Provider: xptr.Ptr(identitystore.ProviderGitHub),
		Subject:  xptr.Ptr("1"),
	})
	require.NoError(t, err)

	tests := []struct {
		name      string
		params    identitystore.CreateParams
		wantErr   error
		wantCheck string
		wantText  string
	}{
		{
			name: "a name another principal holds is refused",
			params: identitystore.CreateParams{
				Name: "holder",
				Kind: identitystore.KindService,
			},
			wantErr:  store.ErrNameTaken,
			wantText: `a principal is already named "holder"`,
		},
		{
			name: "an identity another principal holds is refused naming the holder",
			params: identitystore.CreateParams{
				Name:     "impostor",
				Kind:     identitystore.KindPerson,
				Provider: xptr.Ptr(identitystore.ProviderGitHub),
				Subject:  xptr.Ptr("1"),
			},
			wantErr:  store.ErrNameTaken,
			wantText: `principal "holder" already holds that identity`,
		},
		{
			name: "a person without an identity is refused",
			params: identitystore.CreateParams{
				Name: "nobody",
				Kind: identitystore.KindPerson,
			},
			wantCheck: "principals_person_identity_check",
		},
		{
			name: "a provider without a subject is refused",
			params: identitystore.CreateParams{
				Name:     "half",
				Kind:     identitystore.KindService,
				Provider: xptr.Ptr(identitystore.ProviderGoogle),
			},
			wantCheck: "principals_identity_check",
		},
		{
			name: "an empty subject is refused",
			params: identitystore.CreateParams{
				Name:     "blank",
				Kind:     identitystore.KindPerson,
				Provider: xptr.Ptr(identitystore.ProviderGoogle),
				Subject:  xptr.Ptr(""),
			},
			wantCheck: "principals_subject_check",
		},
		{
			name: "a name the pattern refuses is refused",
			params: identitystore.CreateParams{
				Name: "Upper",
				Kind: identitystore.KindService,
			},
			wantCheck: "principals_name_check",
		},
		{
			name: "a service principal with no identity is added",
			params: identitystore.CreateParams{
				Name: "ci",
				Kind: identitystore.KindService,
			},
		},
		{
			name: "a service principal proved by a provider is added",
			params: identitystore.CreateParams{
				Name:     "deploy",
				Kind:     identitystore.KindService,
				Provider: xptr.Ptr(identitystore.ProviderGitHub),
				Subject:  xptr.Ptr("repo:owner/repo:ref:refs/heads/main"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			created, err := stores.Principals.Create(ctx, tt.params)

			switch {
			case tt.wantErr != nil:
				require.ErrorIs(t, err, tt.wantErr)
				require.ErrorContains(t, err, tt.wantText)
			case tt.wantCheck != "":
				var pgErr *pgconn.PgError
				require.ErrorAs(t, err, &pgErr)
				assert.Equal(t, tt.wantCheck, pgErr.ConstraintName)
			default:
				require.NoError(t, err)
				assert.True(t, store.IsID(created.ID))
				assert.Equal(t, tt.params.Name, created.Name)
				assert.Equal(t, tt.params.Kind, created.Kind)
				assert.Equal(t, tt.params.Provider, created.Provider)
				assert.Equal(t, tt.params.Subject, created.Subject)
				assert.Nil(t, created.DisabledAt)
			}
		})
	}
}

// TestGetAndListPrincipals covers lookup by name and listing by name.
func TestGetAndListPrincipals(t *testing.T) {
	t.Parallel()

	stores := storetest.OpenOwner(t)
	ctx := t.Context()

	_, err := stores.Principals.GetByName(ctx, "absent")
	require.ErrorIs(t, err, store.ErrNotFound)

	for _, name := range []string{"zed", "amy"} {
		_, err := stores.Principals.Create(ctx, identitystore.CreateParams{Name: name, Kind: identitystore.KindService})
		require.NoError(t, err)
	}

	got, err := stores.Principals.GetByName(ctx, "zed")
	require.NoError(t, err)
	assert.Equal(t, "zed", got.Name)

	listed, err := stores.Principals.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"amy", "zed"}, xslice.Map(listed, func(principal identitystore.Principal) string {
		return principal.Name
	}))
}

// TestSetPrincipalDisabled covers disabling keeping its first time and enabling clearing it.
func TestSetPrincipalDisabled(t *testing.T) {
	t.Parallel()

	stores := storetest.OpenOwner(t)
	ctx := t.Context()

	_, err := stores.Principals.SetDisabled(ctx, "absent", true)
	require.ErrorIs(t, err, store.ErrNotFound)

	_, err = stores.Principals.Create(ctx, identitystore.CreateParams{Name: "ci", Kind: identitystore.KindService})
	require.NoError(t, err)

	disabled, err := stores.Principals.SetDisabled(ctx, "ci", true)
	require.NoError(t, err)
	require.NotNil(t, disabled.DisabledAt)

	again, err := stores.Principals.SetDisabled(ctx, "ci", true)
	require.NoError(t, err)
	require.NotNil(t, again.DisabledAt)
	assert.Equal(t, *disabled.DisabledAt, *again.DisabledAt, "disabling twice keeps the first time")

	enabled, err := stores.Principals.SetDisabled(ctx, "ci", false)
	require.NoError(t, err)
	assert.Nil(t, enabled.DisabledAt)
}

// TestDeletePrincipal covers removal counting and taking the bindings.
func TestDeletePrincipal(t *testing.T) {
	t.Parallel()

	stores := storetest.OpenOwner(t)
	ctx := t.Context()

	_, err := stores.Principals.Delete(ctx, "absent")
	require.ErrorIs(t, err, store.ErrNotFound)

	engine, err := stores.Bindings.GetEngineByName(ctx, stores.Engine.Name)
	require.NoError(t, err)
	principal, err := stores.Principals.Create(ctx, identitystore.CreateParams{Name: "ci", Kind: identitystore.KindService})
	require.NoError(t, err)
	_, err = stores.Bindings.Set(ctx, identitystore.SetParams{PrincipalID: principal.ID, EngineID: engine.ID, Role: principalrole.Viewer})
	require.NoError(t, err)

	removed, err := stores.Principals.Delete(ctx, "ci")
	require.NoError(t, err)
	assert.Equal(t, int64(1), removed)

	listed, err := stores.Bindings.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, listed, "removing a principal takes its bindings with it")
}

// TestPrincipalRolesMatchTheDatabase covers the Go roles and the enum drifting apart.
func TestPrincipalRolesMatchTheDatabase(t *testing.T) {
	t.Parallel()

	stores := storetest.OpenOwner(t)

	values, err := testmodels.New(stores.Pool).ListPrincipalRoleValues(t.Context())
	require.NoError(t, err)

	assert.Equal(t, values, xslice.Map(principalrole.Roles, func(role principalrole.Role) string {
		return string(role)
	}), "identity.principal_role and principalrole.Roles must name the same roles in the same order")
}
