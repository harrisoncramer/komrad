package documenttype_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	documentstore "komrad/internal/store/document"
	documenttypestore "komrad/internal/store/documenttype"
	"komrad/internal/storetest"
	"komrad/lib/xslice"
)

// TestAnEngineStartsWithTheRFCType pins the one type a fresh engine ships with.
func TestAnEngineStartsWithTheRFCType(t *testing.T) {
	stores := storetest.Open(t)

	types, err := stores.DocumentTypes.List(t.Context())
	require.NoError(t, err)

	require.Len(t, types, 1)
	assert.Equal(t, "RFC", types[0].Name)
	assert.NotEmpty(t, types[0].Context)

	versions, err := stores.DocumentTypes.ListVersions(t.Context(), types[0].ID)
	require.NoError(t, err)
	require.Len(t, versions, 1, "the seeded type keeps its first write")
	assert.Equal(t, int32(1), versions[0].Version)
}

// TestCreate pins that types list by name and a name is taken once.
func TestCreate(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()

	created, err := stores.DocumentTypes.Create(ctx, "postmortem", "What broke and why.")
	require.NoError(t, err)
	assert.Equal(t, int32(1), created.Version)

	_, err = stores.DocumentTypes.Create(ctx, "postmortem", "Again.")
	require.ErrorIs(t, err, store.ErrNameTaken)

	types, err := stores.DocumentTypes.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"postmortem", "RFC"}, xslice.Map(types, func(t documenttypestore.Type) string { return t.Name }))
}

// TestUpdate pins which rewrites of a type are taken.
func TestUpdate(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	author := stores.SeedAgent(t, agentstore.Agent{Name: "author"})
	stores.SeedDocumentTypes(t, "report")

	postmortem, err := stores.DocumentTypes.Create(ctx, "postmortem", "What broke and why.")
	require.NoError(t, err)
	doc, err := stores.Documents.Create(ctx, documentstore.CreateParams{
		AgentID: author.ID,
		Name:    "outage",
		Type:    "postmortem",
		Body:    "# Outage",
	})
	require.NoError(t, err)

	tests := []struct {
		name    string
		params  documenttypestore.UpdateParams
		wantErr error
	}{
		{
			name: "a version already moved past is refused",
			params: documenttypestore.UpdateParams{
				ID:      postmortem.ID,
				Name:    "postmortem",
				Context: "stale",
				Version: 7,
			},
			wantErr: documenttypestore.ErrChanged,
		},
		{
			name: "a name another type has is refused",
			params: documenttypestore.UpdateParams{
				ID:      postmortem.ID,
				Name:    "report",
				Context: "clash",
				Version: 1,
			},
			wantErr: store.ErrNameTaken,
		},
		{
			name: "a type that does not exist is not found",
			params: documenttypestore.UpdateParams{
				ID:      "not-an-id",
				Name:    "ghost",
				Context: "none",
				Version: 1,
			},
			wantErr: store.ErrNotFound,
		},
		{
			name: "a rename carries the documents written as it",
			params: documenttypestore.UpdateParams{
				ID:      postmortem.ID,
				Name:    "incident",
				Context: "What broke.",
				Version: 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updated, err := stores.DocumentTypes.Update(ctx, tt.params)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, int32(2), updated.Version)
			assert.Equal(t, tt.params.Context, updated.Context)

			got, err := stores.Documents.Get(ctx, doc.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.params.Name, got.Type)
		})
	}
}

// TestDelete pins which types may be removed.
func TestDelete(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	author := stores.SeedAgent(t, agentstore.Agent{Name: "author"})

	used, err := stores.DocumentTypes.Create(ctx, "postmortem", "What broke and why.")
	require.NoError(t, err)
	unused, err := stores.DocumentTypes.Create(ctx, "notes", "Anything.")
	require.NoError(t, err)
	_, err = stores.Documents.Create(ctx, documentstore.CreateParams{
		AgentID: author.ID,
		Name:    "outage",
		Type:    "postmortem",
		Body:    "# Outage",
	})
	require.NoError(t, err)

	tests := []struct {
		name    string
		id      string
		wantErr error
	}{
		{
			name:    "a type documents are written as is refused",
			id:      used.ID,
			wantErr: documenttypestore.ErrInUse,
		},
		{name: "a type no document is written as is deleted", id: unused.ID},
		{
			name:    "a type that does not exist is not found",
			id:      unused.ID,
			wantErr: store.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := stores.DocumentTypes.Delete(ctx, tt.id)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)

			_, err = stores.DocumentTypes.Get(ctx, tt.id)
			require.ErrorIs(t, err, store.ErrNotFound)
		})
	}
}

// TestListVersions pins that every write of a type is kept as it was written, the newest first,
// and that a refused rewrite keeps nothing.
func TestListVersions(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()

	draft, err := stores.DocumentTypes.Create(ctx, "postmortem", "draft")
	require.NoError(t, err)
	revised, err := stores.DocumentTypes.Update(ctx, documenttypestore.UpdateParams{
		ID:      draft.ID,
		Name:    "postmortem",
		Context: "revised",
		Version: draft.Version,
	})
	require.NoError(t, err)
	_, err = stores.DocumentTypes.Update(ctx, documenttypestore.UpdateParams{
		ID:      draft.ID,
		Name:    "postmortem",
		Context: "stale",
		Version: draft.Version,
	})
	require.ErrorIs(t, err, documenttypestore.ErrChanged)
	_, err = stores.DocumentTypes.Update(ctx, documenttypestore.UpdateParams{
		ID:      draft.ID,
		Name:    "incident",
		Context: "renamed",
		Version: revised.Version,
	})
	require.NoError(t, err)

	versions, err := stores.DocumentTypes.ListVersions(ctx, draft.ID)
	require.NoError(t, err)
	require.Len(t, versions, 3)
	for i, want := range []struct {
		version int32
		name    string
	}{
		{version: 3, name: "incident"},
		{version: 2, name: "postmortem"},
		{version: 1, name: "postmortem"},
	} {
		assert.Equal(t, want.version, versions[i].Version)
		assert.Equal(t, want.name, versions[i].Name)
		assert.Empty(t, versions[i].Context, "a listing carries no contexts")
	}

	first, err := stores.DocumentTypes.GetVersion(ctx, draft.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, "draft", first.Context)

	for _, missing := range []struct {
		id      string
		version int32
	}{
		{id: draft.ID, version: 4},
		{id: uuid.NewString(), version: 1},
		{id: "not-an-id", version: 1},
	} {
		_, err := stores.DocumentTypes.GetVersion(ctx, missing.id, missing.version)
		assert.ErrorIs(t, err, store.ErrNotFound, "%s at %d", missing.id, missing.version)
	}
}

// TestListWatched pins the watched set to every type, or the ones keyed.
func TestListWatched(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()

	postmortem, err := stores.DocumentTypes.Create(ctx, "postmortem", "What broke and why.")
	require.NoError(t, err)

	names := func(types []documenttypestore.Type) []string {
		return xslice.Map(types, func(t documenttypestore.Type) string { return t.Name })
	}

	every, err := stores.DocumentTypes.ListWatched(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"postmortem", "RFC"}, names(every))

	keyed, err := stores.DocumentTypes.ListWatched(ctx, []string{postmortem.ID})
	require.NoError(t, err)
	assert.Equal(t, []string{"postmortem"}, names(keyed))

	none, err := stores.DocumentTypes.ListWatched(ctx, []string{})
	require.NoError(t, err)
	assert.Empty(t, none)
}
