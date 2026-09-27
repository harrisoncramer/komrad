package principals_test

import (
	"testing"

	"komrad/internal/principalrole"
	"komrad/internal/principals"
	"komrad/internal/store"
	identitystore "komrad/internal/store/identity"
	"komrad/internal/storetest"
	"komrad/lib/xptr"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openService(t *testing.T) (*principals.PrincipalService, *storetest.OwnerStores) {
	t.Helper()

	stores := storetest.OpenOwner(t)

	return principals.NewPrincipalService(principals.NewPrincipalServiceParams{
		Principals: stores.Principals,
		Bindings:   stores.Bindings,
	}), stores
}

// TestAdd covers the name and identity checks made before the store is asked.
func TestAdd(t *testing.T) {
	t.Parallel()

	service, _ := openService(t)

	tests := []struct {
		name         string
		params       principals.AddParams
		wantErr      error
		wantProvider *identitystore.Provider
		wantSubject  *string
	}{
		{
			name: "a name with a capital is refused",
			params: principals.AddParams{
				Name: "Harry",
				Kind: identitystore.KindService,
			},
			wantErr: principals.ErrBadName,
		},
		{
			name: "a name starting with a digit is refused",
			params: principals.AddParams{
				Name: "1ci",
				Kind: identitystore.KindService,
			},
			wantErr: principals.ErrBadName,
		},
		{
			name: "a person without an identity is refused",
			params: principals.AddParams{
				Name: "harry",
				Kind: identitystore.KindPerson,
			},
			wantErr: principals.ErrBadIdentity,
		},
		{
			name: "a provider without a subject is refused",
			params: principals.AddParams{
				Name:     "harry",
				Kind:     identitystore.KindPerson,
				Provider: "github",
			},
			wantErr: principals.ErrBadIdentity,
		},
		{
			name: "a subject without a provider is refused",
			params: principals.AddParams{
				Name:    "ci",
				Kind:    identitystore.KindService,
				Subject: "1",
			},
			wantErr: principals.ErrBadIdentity,
		},
		{
			name: "a provider komrad does not know is refused",
			params: principals.AddParams{
				Name:     "harry",
				Kind:     identitystore.KindPerson,
				Provider: "okta",
				Subject:  "1",
			},
			wantErr: principals.ErrUnknownProvider,
		},
		{
			name: "a service with no identity is added",
			params: principals.AddParams{
				Name: "ci",
				Kind: identitystore.KindService,
			},
		},
		{
			name: "a person with an identity is added",
			params: principals.AddParams{
				Name:     "harry",
				Kind:     identitystore.KindPerson,
				Provider: "github",
				Subject:  "1",
			},
			wantProvider: xptr.Ptr(identitystore.ProviderGitHub),
			wantSubject:  xptr.Ptr("1"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			added, err := service.Add(t.Context(), tt.params)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.params.Name, added.Name)
			assert.Equal(t, tt.params.Kind, added.Kind)
			assert.Equal(t, tt.wantProvider, added.Provider)
			assert.Equal(t, tt.wantSubject, added.Subject)
		})
	}
}

// TestBind covers binding by name and the role a binding replaces.
func TestBind(t *testing.T) {
	service, stores := openService(t)
	ctx := t.Context()

	_, err := service.Add(ctx, principals.AddParams{Name: "ci", Kind: identitystore.KindService})
	require.NoError(t, err)

	tests := []struct {
		name         string
		params       principals.BindParams
		wantErr      error
		wantPrevious principalrole.Role
	}{
		{
			name: "a role komrad does not have is refused",
			params: principals.BindParams{
				Principal: "ci",
				Engine:    stores.Engine.Name,
				Role:      "admin",
			},
			wantErr: principalrole.ErrUnknown,
		},
		{
			name: "a principal nobody added is refused",
			params: principals.BindParams{
				Principal: "absent",
				Engine:    stores.Engine.Name,
				Role:      "viewer",
			},
			wantErr: store.ErrNotFound,
		},
		{
			name: "an engine nobody ensured is refused",
			params: principals.BindParams{
				Principal: "ci",
				Engine:    "absent",
				Role:      "viewer",
			},
			wantErr: store.ErrNotFound,
		},
		{
			name: "a first binding replaces nothing",
			params: principals.BindParams{
				Principal: "ci",
				Engine:    stores.Engine.Name,
				Role:      "viewer",
			},
		},
		{
			name: "a second binding names the role it replaced",
			params: principals.BindParams{
				Principal: "ci",
				Engine:    stores.Engine.Name,
				Role:      "operator",
			},
			wantPrevious: principalrole.Viewer,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous, err := service.Bind(ctx, tt.params)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantPrevious, previous)
		})
	}
}

// TestUnbind covers unbinding by name.
func TestUnbind(t *testing.T) {
	service, stores := openService(t)
	ctx := t.Context()

	_, err := service.Add(ctx, principals.AddParams{Name: "ci", Kind: identitystore.KindService})
	require.NoError(t, err)
	_, err = service.Bind(ctx, principals.BindParams{Principal: "ci", Engine: stores.Engine.Name, Role: "viewer"})
	require.NoError(t, err)

	tests := []struct {
		name      string
		principal string
		engine    string
		wantErr   error
	}{
		{
			name:      "a principal nobody added is refused",
			principal: "absent",
			engine:    stores.Engine.Name,
			wantErr:   store.ErrNotFound,
		},
		{
			name:      "an engine nobody ensured is refused",
			principal: "ci",
			engine:    "absent",
			wantErr:   store.ErrNotFound,
		},
		{
			name:      "a bound principal is unbound",
			principal: "ci",
			engine:    stores.Engine.Name,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := service.Unbind(ctx, tt.principal, tt.engine)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)

			listed, err := service.List(ctx)
			require.NoError(t, err)
			require.Len(t, listed, 1)
			assert.Empty(t, listed[0].Bindings)
		})
	}
}

// TestListGroupsBindingsUnderTheirPrincipal covers a principal with no bindings listed beside ones with some.
func TestListGroupsBindingsUnderTheirPrincipal(t *testing.T) {
	t.Parallel()

	service, stores := openService(t)
	ctx := t.Context()

	for _, name := range []string{"deploy", "ci", "idle"} {
		_, err := service.Add(ctx, principals.AddParams{Name: name, Kind: identitystore.KindService})
		require.NoError(t, err)
	}

	_, err := service.Bind(ctx, principals.BindParams{Principal: "ci", Engine: stores.Engine.Name, Role: "automation"})
	require.NoError(t, err)
	_, err = service.Bind(ctx, principals.BindParams{Principal: "deploy", Engine: stores.Engine.Name, Role: "operator"})
	require.NoError(t, err)

	listed, err := service.List(ctx)
	require.NoError(t, err)

	got := xslice.Map(listed, func(listed principals.Listed) []string {
		return append([]string{listed.Principal.Name}, xslice.Map(listed.Bindings, func(binding identitystore.Binding) string {
			return binding.EngineName + ":" + string(binding.Role)
		})...)
	})
	assert.Equal(t, [][]string{
		{"ci", stores.Engine.Name + ":automation"},
		{"deploy", stores.Engine.Name + ":operator"},
		{"idle"},
	}, got)
}
