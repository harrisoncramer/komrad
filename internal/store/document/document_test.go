package document_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	documentstore "komrad/internal/store/document"
	"komrad/internal/storetest"
	"komrad/lib/xptr"
)

// TestCreate pins which names a new document may take.
func TestCreate(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	author := stores.SeedAgent(t, agentstore.Agent{Name: "author"})
	other := stores.SeedAgent(t, agentstore.Agent{Name: "other"})

	first, err := stores.Documents.Create(ctx, documentstore.CreateParams{
		AgentID: author.ID,
		Name:    "Documents",
		Kind:    documentstore.KindRFC,
		Body:    "# Documents",
	})
	require.NoError(t, err)

	tests := []struct {
		name           string
		params         documentstore.CreateParams
		wantErr        error
		wantID         bool
		wantConstraint string
	}{
		{
			name: "a name the agent already uses is refused, naming the document that holds it",
			params: documentstore.CreateParams{
				AgentID: author.ID,
				Name:    "Documents",
				Kind:    documentstore.KindNotes,
				Body:    "again",
			},
			wantErr: store.ErrNameTaken,
			wantID:  true,
		},
		{
			name: "another agent may use the same name",
			params: documentstore.CreateParams{
				AgentID: other.ID,
				Name:    "Documents",
				Kind:    documentstore.KindNotes,
				Body:    "mine",
			},
		},
		{
			name: "a second name for the same agent is its own document",
			params: documentstore.CreateParams{
				AgentID: author.ID,
				Name:    "Findings",
				Kind:    documentstore.KindReport,
				Body:    "found",
			},
		},
		{
			name: "an empty body is refused",
			params: documentstore.CreateParams{
				AgentID: author.ID,
				Name:    "Blank",
				Kind:    documentstore.KindNotes,
			},
			wantConstraint: "documents_body_written",
		},
		{
			name: "a document a person creates keeps its first version as theirs",
			params: documentstore.CreateParams{
				AgentID:  author.ID,
				Name:     "Plan",
				Kind:     documentstore.KindRFC,
				Body:     "Empty RFC document.",
				ByPerson: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			created, err := stores.Documents.Create(ctx, tt.params)
			if tt.wantConstraint != "" {
				require.ErrorContains(t, err, tt.wantConstraint)

				return
			}
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				if tt.wantID {
					assert.Contains(t, err.Error(), first.ID)
				}

				return
			}
			require.NoError(t, err)

			got, err := stores.Documents.Get(ctx, created.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.params.Body, got.Body)
			assert.Equal(t, tt.params.Kind, got.Kind)

			versions, err := stores.Documents.ListVersions(ctx, created.ID)
			require.NoError(t, err)
			require.Len(t, versions, 1)
			assert.Equal(t, int32(1), versions[0].Version)
			assert.Equal(t, tt.params.ByPerson, versions[0].ByPerson)
		})
	}
}

// TestUpdate pins which rewrites land and that a refused one changes nothing.
func TestUpdate(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	author := stores.SeedAgent(t, agentstore.Agent{Name: "author"})
	other := stores.SeedAgent(t, agentstore.Agent{Name: "other"})

	create := func(t *testing.T, name string) *documentstore.Document {
		t.Helper()

		created, err := stores.Documents.Create(ctx, documentstore.CreateParams{
			AgentID: author.ID,
			Name:    name,
			Kind:    documentstore.KindRFC,
			Body:    "draft",
		})
		require.NoError(t, err)

		return created
	}

	tests := []struct {
		name    string
		params  func(doc *documentstore.Document) documentstore.UpdateParams
		wantErr error
	}{
		{
			name: "the name, kind and body all change and the document keeps its id",
			params: func(doc *documentstore.Document) documentstore.UpdateParams {
				return documentstore.UpdateParams{
					ID:      doc.ID,
					AgentID: author.ID,
					Name:    "renamed",
					Kind:    documentstore.KindReport,
					Body:    "final",
					Version: doc.Version,
				}
			},
		},
		{
			name: "another agent's document is not found",
			params: func(doc *documentstore.Document) documentstore.UpdateParams {
				return documentstore.UpdateParams{
					ID:      doc.ID,
					AgentID: other.ID,
					Name:    doc.Name,
					Kind:    doc.Kind,
					Body:    "stolen",
					Version: doc.Version,
				}
			},
			wantErr: store.ErrNotFound,
		},
		{
			name: "an id no document holds is not found",
			params: func(doc *documentstore.Document) documentstore.UpdateParams {
				return documentstore.UpdateParams{
					ID:      uuid.NewString(),
					AgentID: author.ID,
					Name:    doc.Name,
					Kind:    doc.Kind,
					Body:    "lost",
					Version: doc.Version,
				}
			},
			wantErr: store.ErrNotFound,
		},
		{
			name: "a value that is not an id is not found",
			params: func(doc *documentstore.Document) documentstore.UpdateParams {
				return documentstore.UpdateParams{
					ID:      "not-an-id",
					AgentID: author.ID,
					Name:    doc.Name,
					Kind:    doc.Kind,
					Body:    "lost",
					Version: doc.Version,
				}
			},
			wantErr: store.ErrNotFound,
		},
		{
			name: "a name another of the agent's documents holds is refused",
			params: func(doc *documentstore.Document) documentstore.UpdateParams {
				return documentstore.UpdateParams{
					ID:      doc.ID,
					AgentID: author.ID,
					Name:    "taken",
					Kind:    doc.Kind,
					Body:    "clash",
					Version: doc.Version,
				}
			},
			wantErr: store.ErrNameTaken,
		},
		{
			name: "a version the document is no longer at is refused",
			params: func(doc *documentstore.Document) documentstore.UpdateParams {
				return documentstore.UpdateParams{
					ID:      doc.ID,
					AgentID: author.ID,
					Name:    doc.Name,
					Kind:    doc.Kind,
					Body:    "stale",
					Version: doc.Version + 1,
				}
			},
			wantErr: documentstore.ErrChanged,
		},
	}

	create(t, "taken")
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := create(t, fmt.Sprintf("document-%d", i))
			params := tt.params(doc)

			updated, err := stores.Documents.Update(ctx, params)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				unchanged, err := stores.Documents.Get(ctx, doc.ID)
				require.NoError(t, err)
				assert.Equal(t, "draft", unchanged.Body)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, doc.ID, updated.ID)
			assert.Equal(t, params.Name, updated.Name)
			assert.Equal(t, params.Kind, updated.Kind)
			assert.Equal(t, params.Body, updated.Body)
			assert.Equal(t, doc.Version+1, updated.Version)
			assert.False(t, updated.UpdatedAt.Before(doc.UpdatedAt))
		})
	}
}

// TestGet pins that an id holding no document is not found.
func TestGet(t *testing.T) {
	stores := storetest.Open(t)

	for _, id := range []string{uuid.NewString(), "not-an-id"} {
		_, err := stores.Documents.Get(t.Context(), id)
		assert.ErrorIs(t, err, store.ErrNotFound, id)
	}
}

// TestList pins whose documents a listing holds and in what order.
func TestList(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	author := stores.SeedAgent(t, agentstore.Agent{Name: "author"})
	other := stores.SeedAgent(t, agentstore.Agent{Name: "other"})

	write := func(agentID, name string, kind documentstore.Kind) string {
		created, err := stores.Documents.Create(ctx, documentstore.CreateParams{
			AgentID: agentID,
			Name:    name,
			Kind:    kind,
			Body:    "body of " + name,
		})
		require.NoError(t, err)

		return created.ID
	}
	rfc := write(author.ID, "design", documentstore.KindRFC)
	notes := write(author.ID, "scratch", documentstore.KindNotes)
	write(other.ID, "elsewhere", documentstore.KindRFC)

	_, err := stores.Documents.Update(ctx, documentstore.UpdateParams{
		ID:      rfc,
		AgentID: author.ID,
		Name:    "design",
		Kind:    documentstore.KindRFC,
		Body:    "revised",
		Version: 1,
	})
	require.NoError(t, err)

	tests := []struct {
		name    string
		agentID string
		kind    *documentstore.Kind
		want    []string
	}{
		{
			name:    "an agent's own documents, the newest write first",
			agentID: author.ID,
			want:    []string{rfc, notes},
		},
		{
			name:    "narrowed to one kind",
			agentID: author.ID,
			kind:    xptr.Ptr(documentstore.KindNotes),
			want:    []string{notes},
		},
		{name: "an agent with none", agentID: uuid.NewString()},
		{name: "a value that is not an id", agentID: "not-an-id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listed, err := stores.Documents.List(ctx, tt.agentID, tt.kind)
			require.NoError(t, err)

			ids := make([]string, 0, len(listed))
			for _, doc := range listed {
				ids = append(ids, doc.ID)
				assert.Empty(t, doc.Body, "a listing carries no bodies")
			}
			assert.Len(t, ids, len(tt.want))
			if len(tt.want) > 0 {
				assert.Equal(t, tt.want, ids)
			}
		})
	}
}

// TestDeletingTheAgentDeletesItsDocuments pins that archiving keeps an agent's documents and deleting it removes them.
func TestDeletingTheAgentDeletesItsDocuments(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	author := stores.SeedAgent(t, agentstore.Agent{Name: "author"})

	created, err := stores.Documents.Create(ctx, documentstore.CreateParams{
		AgentID: author.ID,
		Name:    "gone",
		Kind:    documentstore.KindNotes,
		Body:    "soon",
	})
	require.NoError(t, err)

	_, err = stores.Agents.Archive(ctx, []string{author.ID})
	require.NoError(t, err)
	_, err = stores.Documents.Get(ctx, created.ID)
	require.NoError(t, err, "archiving keeps the document")

	_, err = stores.Agents.Delete(ctx, []string{author.ID})
	require.NoError(t, err)
	_, err = stores.Documents.Get(ctx, created.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
	versions, err := stores.Documents.ListVersions(ctx, created.ID)
	require.NoError(t, err)
	assert.Empty(t, versions, "and every write of it")
}

// TestListVersions pins that every write of a document is kept as it was written, the newest first,
// and that a refused rewrite keeps nothing.
func TestListVersions(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	author := stores.SeedAgent(t, agentstore.Agent{Name: "author"})

	draft, err := stores.Documents.Create(ctx, documentstore.CreateParams{
		AgentID: author.ID,
		Name:    "design",
		Kind:    documentstore.KindNotes,
		Body:    "draft",
	})
	require.NoError(t, err)
	revised, err := stores.Documents.Update(ctx, documentstore.UpdateParams{
		ID:      draft.ID,
		AgentID: author.ID,
		Name:    "design",
		Kind:    documentstore.KindRFC,
		Body:    "revised",
		Version: draft.Version,
	})
	require.NoError(t, err)
	_, err = stores.Documents.Update(ctx, documentstore.UpdateParams{
		ID:      draft.ID,
		AgentID: author.ID,
		Name:    "design",
		Kind:    documentstore.KindRFC,
		Body:    "stale",
		Version: draft.Version,
	})
	require.ErrorIs(t, err, documentstore.ErrChanged)
	_, err = stores.Documents.Update(ctx, documentstore.UpdateParams{
		ID:       draft.ID,
		AgentID:  author.ID,
		Name:     "design, edited",
		Kind:     documentstore.KindRFC,
		Body:     "edited",
		Version:  revised.Version,
		ByPerson: true,
	})
	require.NoError(t, err)

	versions, err := stores.Documents.ListVersions(ctx, draft.ID)
	require.NoError(t, err)
	require.Len(t, versions, 3)
	for i, want := range []struct {
		version  int32
		name     string
		kind     documentstore.Kind
		byPerson bool
	}{
		{
			version:  3,
			name:     "design, edited",
			kind:     documentstore.KindRFC,
			byPerson: true,
		},
		{
			version: 2,
			name:    "design",
			kind:    documentstore.KindRFC,
		},
		{
			version: 1,
			name:    "design",
			kind:    documentstore.KindNotes,
		},
	} {
		assert.Equal(t, want.version, versions[i].Version)
		assert.Equal(t, want.name, versions[i].Name)
		assert.Equal(t, want.kind, versions[i].Kind)
		assert.Equal(t, want.byPerson, versions[i].ByPerson)
		assert.Empty(t, versions[i].Body, "a listing carries no bodies")
	}

	first, err := stores.Documents.GetVersion(ctx, draft.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, "draft", first.Body)

	for _, missing := range []struct {
		id      string
		version int32
	}{
		{id: draft.ID, version: 4},
		{id: uuid.NewString(), version: 1},
		{id: "not-an-id", version: 1},
	} {
		_, err := stores.Documents.GetVersion(ctx, missing.id, missing.version)
		assert.ErrorIs(t, err, store.ErrNotFound, "%s at %d", missing.id, missing.version)
	}
}
