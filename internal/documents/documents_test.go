package documents_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"komrad/internal/documents"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	documentstore "komrad/internal/store/document"
	"komrad/internal/storetest"
	"komrad/lib/xptr"
)

// TestWrite pins where a body comes from and which bodies are refused.
func TestWrite(t *testing.T) {
	stores := storetest.Open(t)
	cwd := t.TempDir()
	author := stores.SeedAgent(t, agentstore.Agent{Name: "author", Cwd: cwd})
	service := documents.NewDocumentService(documents.NewDocumentServiceParams{Agents: stores.Agents, Documents: stores.Documents})

	writeFile := func(name, content string) string {
		path := filepath.Join(cwd, name)
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		return path
	}
	require.NoError(t, os.Mkdir(filepath.Join(cwd, "folder"), 0o700))

	tests := []struct {
		name     string
		params   documents.WriteDocumentParams
		wantErr  error
		wantBody string
	}{
		{
			name:     "a body is stored as given",
			params:   documents.WriteDocumentParams{Name: "given", Body: "# Given"},
			wantBody: "# Given",
		},
		{
			name:     "an absolute path is read in",
			params:   documents.WriteDocumentParams{Name: "absolute", Path: writeFile("absolute.md", "# Absolute")},
			wantBody: "# Absolute",
		},
		{
			name:     "a relative path is read from the agent's working directory",
			params:   documents.WriteDocumentParams{Name: "relative", Path: filepath.Base(writeFile("relative.md", "# Relative"))},
			wantBody: "# Relative",
		},
		{
			name:    "a path that names nothing is refused",
			params:  documents.WriteDocumentParams{Name: "missing", Path: "missing.md"},
			wantErr: documents.ErrUnreadablePath,
		},
		{
			name:    "a path naming a directory is refused",
			params:  documents.WriteDocumentParams{Name: "folder", Path: "folder"},
			wantErr: documents.ErrUnreadablePath,
		},
		{
			name:    "a file over the cap is refused before it is read",
			params:  documents.WriteDocumentParams{Name: "huge", Path: writeFile("huge.md", strings.Repeat("a", documents.MaxBodyBytes+1))},
			wantErr: documents.ErrTooLarge,
		},
		{
			name:    "a body over the cap is refused",
			params:  documents.WriteDocumentParams{Name: "huge", Body: strings.Repeat("a", documents.MaxBodyBytes+1)},
			wantErr: documents.ErrTooLarge,
		},
		{
			name:    "a body that is not UTF-8 is refused",
			params:  documents.WriteDocumentParams{Name: "binary", Path: writeFile("binary.md", "\xff\xfe")},
			wantErr: documents.ErrNotUTF8,
		},
		{
			name:    "neither a body nor a path is refused",
			params:  documents.WriteDocumentParams{Name: "empty"},
			wantErr: documents.ErrNoBody,
		},
		{
			name:    "an agent that does not exist is not found",
			params:  documents.WriteDocumentParams{AgentID: uuid.NewString(), Name: "orphan", Body: "# Orphan"},
			wantErr: store.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := tt.params
			if params.AgentID == "" {
				params.AgentID = author.ID
			}
			params.Kind = documentstore.KindNotes

			written, err := service.WriteDocument(t.Context(), params)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)

			stored, err := stores.Documents.Get(t.Context(), written.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantBody, stored.Body)
		})
	}
}

// TestWriteWithAnIDRewritesThatDocument pins that a write naming an id changes that document rather than adding one.
func TestWriteWithAnIDRewritesThatDocument(t *testing.T) {
	stores := storetest.Open(t)
	author := stores.SeedAgent(t, agentstore.Agent{Name: "author"})
	service := documents.NewDocumentService(documents.NewDocumentServiceParams{Agents: stores.Agents, Documents: stores.Documents})
	ctx := t.Context()

	draft, err := service.WriteDocument(ctx, documents.WriteDocumentParams{AgentID: author.ID, Name: "Documents", Kind: documentstore.KindRFC, Body: "draft"})
	require.NoError(t, err)

	revised, err := service.WriteDocument(ctx, documents.WriteDocumentParams{
		AgentID: author.ID,
		ID:      xptr.Ptr(draft.ID),
		Name:    "Documents RFC",
		Kind:    documentstore.KindRFC,
		Body:    "revised",
	})
	require.NoError(t, err)
	assert.Equal(t, draft.ID, revised.ID)

	listed, err := service.ListDocuments(ctx, author.ID, nil)
	require.NoError(t, err)
	require.Len(t, listed, 1, "a retitled document is the same document")
	assert.Equal(t, "Documents RFC", listed[0].Name)
}
