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
	textsearchstore "komrad/internal/store/textsearch"
	"komrad/internal/storetest"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
)

// TestCreate pins which names a new document may take.
func TestCreate(t *testing.T) {
	stores := storetest.Open(t)
	stores.SeedDocumentTypes(t, "notes", "report")
	ctx := t.Context()
	author := stores.SeedAgent(t, agentstore.Agent{Name: "author"})
	other := stores.SeedAgent(t, agentstore.Agent{Name: "other"})

	first, err := stores.Documents.Create(ctx, documentstore.CreateParams{
		AgentID: author.ID,
		Name:    "Documents",
		Type:    "RFC",
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
				Type:    "notes",
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
				Type:    "notes",
				Body:    "mine",
			},
		},
		{
			name: "a second name for the same agent is its own document",
			params: documentstore.CreateParams{
				AgentID: author.ID,
				Name:    "Findings",
				Type:    "report",
				Body:    "found",
			},
		},
		{
			name: "a type that does not exist is refused",
			params: documentstore.CreateParams{
				AgentID: author.ID,
				Name:    "Untyped",
				Type:    "postmortem",
				Body:    "# Untyped",
			},
			wantErr: documentstore.ErrUnknownType,
		},
		{
			name: "an empty body is refused",
			params: documentstore.CreateParams{
				AgentID: author.ID,
				Name:    "Blank",
				Type:    "notes",
			},
			wantConstraint: "documents_body_written",
		},
		{
			name: "a document a person creates keeps its first version as theirs",
			params: documentstore.CreateParams{
				AgentID:  author.ID,
				Name:     "Plan",
				Type:     "RFC",
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
					require.ErrorContains(t, err, first.ID)
				}

				return
			}
			require.NoError(t, err)

			got, err := stores.Documents.Get(ctx, created.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.params.Body, got.Body)
			assert.Equal(t, tt.params.Type, got.Type)

			versions, err := stores.Documents.ListVersions(ctx, created.ID)
			require.NoError(t, err)
			require.Len(t, versions, 1)
			assert.Equal(t, int32(1), versions[0].Version)
			assert.Equal(t, tt.params.ByPerson, versions[0].ByPerson)
		})
	}
}

// TestCreateRecordsTheAuthorsTask pins that a document carries the task its author works on, and an orchestrator's carries none.
func TestCreateRecordsTheAuthorsTask(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	worker := stores.SeedAgent(t, agentstore.Agent{Name: "worker"})
	orchestrator := stores.SeedAgent(t, agentstore.Agent{Name: "orchestrator", Role: agentstore.RoleOrchestrator})

	tests := []struct {
		name     string
		author   *agentstore.Agent
		wantTask *string
	}{
		{
			name:   "an orchestrator's document",
			author: orchestrator,
		},
		{
			name:     "a document written by an agent on a task",
			author:   worker,
			wantTask: worker.TaskID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			created, err := stores.Documents.Create(ctx, documentstore.CreateParams{
				AgentID: tt.author.ID,
				Name:    "Plan",
				Type:    "RFC",
				Body:    "# Plan",
			})
			require.NoError(t, err)

			got, err := stores.Documents.Get(ctx, created.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantTask, got.TaskID)
		})
	}
}

// TestUpdate pins which rewrites land and that a refused one changes nothing.
func TestUpdate(t *testing.T) {
	stores := storetest.Open(t)
	stores.SeedDocumentTypes(t, "notes", "report")
	ctx := t.Context()
	author := stores.SeedAgent(t, agentstore.Agent{Name: "author"})
	other := stores.SeedAgent(t, agentstore.Agent{Name: "other"})

	create := func(t *testing.T, name string) *documentstore.Document {
		t.Helper()

		created, err := stores.Documents.Create(ctx, documentstore.CreateParams{
			AgentID: author.ID,
			Name:    name,
			Type:    "RFC",
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
			name: "the name, type and body all change and the document keeps its id",
			params: func(doc *documentstore.Document) documentstore.UpdateParams {
				return documentstore.UpdateParams{
					ID:      doc.ID,
					AgentID: author.ID,
					Name:    "renamed",
					Type:    "report",
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
					Type:    doc.Type,
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
					Type:    doc.Type,
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
					Type:    doc.Type,
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
					Type:    doc.Type,
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
					Type:    doc.Type,
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
			assert.Equal(t, params.Type, updated.Type)
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

// TestDelete pins that deleting a document takes every write of it, and that an id holding none is not found.
func TestDelete(t *testing.T) {
	stores := storetest.Open(t)
	stores.SeedDocumentTypes(t, "notes")
	ctx := t.Context()
	author := stores.SeedAgent(t, agentstore.Agent{Name: "author"})

	created, err := stores.Documents.Create(ctx, documentstore.CreateParams{
		AgentID: author.ID,
		Name:    "gone",
		Type:    "notes",
		Body:    "soon",
	})
	require.NoError(t, err)

	require.NoError(t, stores.Documents.Delete(ctx, created.ID))

	_, err = stores.Documents.Get(ctx, created.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
	versions, err := stores.Documents.ListVersions(ctx, created.ID)
	require.NoError(t, err)
	assert.Empty(t, versions, "and every write of it")

	for _, id := range []string{created.ID, uuid.NewString(), "not-an-id"} {
		assert.ErrorIs(t, stores.Documents.Delete(ctx, id), store.ErrNotFound, id)
	}
}

// TestList pins whose documents a listing holds and in what order.
func TestList(t *testing.T) {
	stores := storetest.Open(t)
	stores.SeedDocumentTypes(t, "notes", "report")
	ctx := t.Context()
	author := stores.SeedAgent(t, agentstore.Agent{Name: "author"})
	other := stores.SeedAgent(t, agentstore.Agent{Name: "other"})
	leaving := stores.SeedAgent(t, agentstore.Agent{Name: "leaving"})
	reviewer := stores.SeedAgent(t, agentstore.Agent{
		Name:   "reviewer",
		Role:   agentstore.RoleReviewer,
		TaskID: author.TaskID,
	})

	write := func(agentID, name string, typeName string) string {
		created, err := stores.Documents.Create(ctx, documentstore.CreateParams{
			AgentID: agentID,
			Name:    name,
			Type:    typeName,
			Body:    "body of " + name,
		})
		require.NoError(t, err)

		return created.ID
	}
	rfc := write(author.ID, "design", "RFC")
	notes := write(author.ID, "scratch", "notes")
	elsewhere := write(other.ID, "elsewhere", "RFC")
	write(leaving.ID, "doomed", "RFC")
	findings := write(reviewer.ID, "findings", "notes")

	_, err := stores.Documents.Update(ctx, documentstore.UpdateParams{
		ID:      rfc,
		AgentID: author.ID,
		Name:    "design",
		Type:    "RFC",
		Body:    "revised",
		Version: 1,
	})
	require.NoError(t, err)
	_, err = stores.Agents.Archive(ctx, []string{other.ID})
	require.NoError(t, err)
	_, err = stores.Agents.MarkDeleting(ctx, []string{leaving.ID})
	require.NoError(t, err)

	tests := []struct {
		name     string
		agentID  *string
		taskID   *string
		typeName *string
		want     []string
	}{
		{
			name:    "an agent's own documents, the newest write first",
			agentID: xptr.Ptr(author.ID),
			want:    []string{rfc, notes},
		},
		{
			name:     "narrowed to one type",
			agentID:  xptr.Ptr(author.ID),
			typeName: xptr.Ptr("notes"),
			want:     []string{notes},
		},
		{
			name: "every agent's documents, archived agents included",
			want: []string{rfc, findings, elsewhere, notes},
		},
		{
			name:     "every agent's documents of one type",
			typeName: xptr.Ptr("RFC"),
			want:     []string{rfc, elsewhere},
		},
		{
			name:   "a task's documents, from every agent that worked on it",
			taskID: author.TaskID,
			want:   []string{rfc, findings, notes},
		},
		{
			name:     "a task's documents of one type",
			taskID:   author.TaskID,
			typeName: xptr.Ptr("notes"),
			want:     []string{findings, notes},
		},
		{name: "a task with none", taskID: xptr.Ptr(uuid.NewString())},
		{name: "a task that is not an id", taskID: xptr.Ptr("not-an-id")},
		{name: "an agent with none", agentID: xptr.Ptr(uuid.NewString())},
		{name: "an agent being deleted", agentID: xptr.Ptr(leaving.ID)},
		{name: "a value that is not an id", agentID: xptr.Ptr("not-an-id")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listed, err := stores.Documents.List(ctx, documentstore.ListParams{
				AgentID: tt.agentID,
				TaskID:  tt.taskID,
				Type:    tt.typeName,
				Limit:   10,
			})
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

	first, err := stores.Documents.List(ctx, documentstore.ListParams{Limit: 1})
	require.NoError(t, err)
	require.Len(t, first, 1)

	rest, err := stores.Documents.List(ctx, documentstore.ListParams{
		Limit:  10,
		Cursor: &documentstore.Cursor{UpdatedAt: first[0].UpdatedAt, ID: first[0].ID},
	})
	require.NoError(t, err)

	paged := append([]string{first[0].ID}, xslice.Map(rest, func(doc documentstore.Document) string { return doc.ID })...)
	assert.Equal(t, []string{rfc, findings, elsewhere, notes}, paged, "a cursor carries on where its page left off")
}

// TestDeletingTheAgentDeletesItsDocuments pins that archiving keeps an agent's documents and deleting it removes them.
func TestDeletingTheAgentDeletesItsDocuments(t *testing.T) {
	stores := storetest.Open(t)
	stores.SeedDocumentTypes(t, "notes", "report")
	ctx := t.Context()
	author := stores.SeedAgent(t, agentstore.Agent{Name: "author"})

	created, err := stores.Documents.Create(ctx, documentstore.CreateParams{
		AgentID: author.ID,
		Name:    "gone",
		Type:    "notes",
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
	stores.SeedDocumentTypes(t, "notes", "report")
	ctx := t.Context()
	author := stores.SeedAgent(t, agentstore.Agent{Name: "author"})

	draft, err := stores.Documents.Create(ctx, documentstore.CreateParams{
		AgentID: author.ID,
		Name:    "design",
		Type:    "notes",
		Body:    "draft",
	})
	require.NoError(t, err)
	revised, err := stores.Documents.Update(ctx, documentstore.UpdateParams{
		ID:      draft.ID,
		AgentID: author.ID,
		Name:    "design",
		Type:    "RFC",
		Body:    "revised",
		Version: draft.Version,
	})
	require.NoError(t, err)
	_, err = stores.Documents.Update(ctx, documentstore.UpdateParams{
		ID:      draft.ID,
		AgentID: author.ID,
		Name:    "design",
		Type:    "RFC",
		Body:    "stale",
		Version: draft.Version,
	})
	require.ErrorIs(t, err, documentstore.ErrChanged)
	_, err = stores.Documents.Update(ctx, documentstore.UpdateParams{
		ID:       draft.ID,
		AgentID:  author.ID,
		Name:     "design, edited",
		Type:     "RFC",
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
		typeName string
		byPerson bool
	}{
		{
			version:  3,
			name:     "design, edited",
			typeName: "RFC",
			byPerson: true,
		},
		{
			version:  2,
			name:     "design",
			typeName: "RFC",
		},
		{
			version:  1,
			name:     "design",
			typeName: "notes",
		},
	} {
		assert.Equal(t, want.version, versions[i].Version)
		assert.Equal(t, want.name, versions[i].Name)
		assert.Equal(t, want.typeName, versions[i].Type)
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

// TestSearch pins which documents a search of their bodies finds and in what order.
func TestSearch(t *testing.T) {
	stores := storetest.Open(t)
	stores.SeedDocumentTypes(t, "notes")
	ctx := t.Context()
	author := stores.SeedAgent(t, agentstore.Agent{Name: "author"})
	other := stores.SeedAgent(t, agentstore.Agent{Name: "other"})
	leaving := stores.SeedAgent(t, agentstore.Agent{Name: "leaving"})

	write := func(agentID, name, typeName, body string) string {
		created, err := stores.Documents.Create(ctx, documentstore.CreateParams{
			AgentID: agentID,
			Name:    name,
			Type:    typeName,
			Body:    body,
		})
		require.NoError(t, err)

		return created.ID
	}
	plan := write(author.ID, "plan", "RFC", "the migration adds a trigram index on the body")
	notes := write(author.ID, "notes", "notes", "an index is mentioned once")
	elsewhere := write(other.ID, "elsewhere", "RFC", "the trigram index again, trigram index everywhere")
	rewritten := write(author.ID, "rewritten", "RFC", "the old text said trigram")
	write(leaving.ID, "doomed", "RFC", "a trigram index nobody can see")
	near := write(author.ID, "near", "notes", "reindexing happens nightly")

	_, err := stores.Documents.Update(ctx, documentstore.UpdateParams{
		ID:      rewritten,
		AgentID: author.ID,
		Name:    "rewritten",
		Type:    "RFC",
		Body:    "nothing left of it",
		Version: 1,
	})
	require.NoError(t, err)
	_, err = stores.Agents.MarkDeleting(ctx, []string{leaving.ID})
	require.NoError(t, err)

	tests := []struct {
		name        string
		params      documentstore.SearchParams
		wantMatches []string
		wantNear    []string
	}{
		{
			name:        "every document holding the words, the best first",
			params:      documentstore.SearchParams{Query: "trigram index"},
			wantMatches: []string{elsewhere, plan},
		},
		{
			name:        "a word a document holds only inside a longer one comes after as near",
			params:      documentstore.SearchParams{Query: "index"},
			wantMatches: []string{elsewhere, notes, plan},
			wantNear:    []string{near},
		},
		{
			name:        "a word left out drops the documents holding it",
			params:      documentstore.SearchParams{Query: "index -trigram"},
			wantMatches: []string{notes},
			wantNear:    []string{near},
		},
		{
			name:        "narrowed to one agent",
			params:      documentstore.SearchParams{Query: "trigram", AgentID: xptr.Ptr(other.ID)},
			wantMatches: []string{elsewhere},
		},
		{
			name:        "narrowed to one type",
			params:      documentstore.SearchParams{Query: "index", Type: xptr.Ptr("notes")},
			wantMatches: []string{notes},
			wantNear:    []string{near},
		},
		{
			name:        "the limit holds across both sections",
			params:      documentstore.SearchParams{Query: "index", Limit: 3},
			wantMatches: []string{elsewhere, notes, plan},
		},
		{name: "only the current body is searched", params: documentstore.SearchParams{Query: "old text"}},
		{name: "a query wanting no word finds nothing", params: documentstore.SearchParams{Query: "-trigram"}},
		{name: "an agent id that is not an id finds nothing", params: documentstore.SearchParams{Query: "trigram", AgentID: xptr.Ptr("not-an-id")}},
		{
			name:        "a NUL in the query is dropped",
			params:      documentstore.SearchParams{Query: "trig\x00ram", AgentID: xptr.Ptr(other.ID)},
			wantMatches: []string{elsewhere},
		},
	}

	ids := func(hits []documentstore.SearchHit) []string {
		return xslice.Map(hits, func(hit documentstore.SearchHit) string { return hit.ID })
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := stores.Documents.Search(ctx, tt.params)
			require.NoError(t, err)

			assert.Equal(t, tt.wantMatches, nilIfEmpty(ids(results.Matches)))
			assert.Equal(t, tt.wantNear, nilIfEmpty(ids(results.Near)))
		})
	}

	t.Run("a match carries its document and the passage marked", func(t *testing.T) {
		results, err := stores.Documents.Search(ctx, documentstore.SearchParams{Query: "trigram", AgentID: xptr.Ptr(author.ID)})
		require.NoError(t, err)
		require.Len(t, results.Matches, 1)

		hit := results.Matches[0]
		assert.Equal(t, author.ID, hit.AgentID)
		assert.Equal(t, "plan", hit.Name)
		assert.Contains(t, hit.Snippet, textsearchstore.MarkOpen+"trigram"+textsearchstore.MarkClose)
	})

	t.Run("a near hit marks the word inside the longer one", func(t *testing.T) {
		results, err := stores.Documents.Search(ctx, documentstore.SearchParams{Query: "index", Type: xptr.Ptr("notes")})
		require.NoError(t, err)
		require.Len(t, results.Near, 1)

		assert.Contains(t, results.Near[0].Snippet, "re"+textsearchstore.MarkOpen+"index"+textsearchstore.MarkClose+"ing")
	})
}

func nilIfEmpty(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}

	return ids
}
