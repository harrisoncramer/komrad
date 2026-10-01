package documents_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"komrad/internal/documents"
	"komrad/internal/events/eventstest"
	"komrad/internal/store"
	documentstore "komrad/internal/store/document"
	"komrad/internal/storetest"
	"komrad/lib/xptr"
)

// TestGetDocumentType pins that an unknown name is refused with the names that exist.
func TestGetDocumentType(t *testing.T) {
	stores := storetest.Open(t)
	stores.SeedDocumentTypes(t, "notes")
	service := newService(stores, eventstest.NewQueue(t, stores, documents.NewEventHandler()))

	got, err := service.GetDocumentType(t.Context(), "notes", nil)
	require.NoError(t, err)
	assert.Equal(t, "notes", got.Name)

	_, err = service.GetDocumentType(t.Context(), "postmortem", nil)
	require.ErrorIs(t, err, documentstore.ErrUnknownType)
	assert.ErrorContains(t, err, "the types are notes, RFC")
}

// TestWriteDocumentType pins that a rewrite needs the version it replaces.
func TestWriteDocumentType(t *testing.T) {
	stores := storetest.Open(t)
	service := newService(stores, eventstest.NewQueue(t, stores, documents.NewEventHandler()))

	created, err := service.WriteDocumentType(t.Context(), documents.WriteDocumentTypeParams{
		Name:    "postmortem",
		Context: "What broke and why.",
	})
	require.NoError(t, err)

	tests := []struct {
		name    string
		params  documents.WriteDocumentTypeParams
		wantErr error
	}{
		{
			name: "a rewrite with no version is refused",
			params: documents.WriteDocumentTypeParams{
				ID:      &created.ID,
				Name:    "postmortem",
				Context: "Again.",
			},
			wantErr: documents.ErrNoTypeVersion,
		},
		{
			name: "a rewrite from the version read is taken",
			params: documents.WriteDocumentTypeParams{
				ID:      &created.ID,
				Version: xptr.Ptr(int32(1)),
				Name:    "incident",
				Context: "What broke.",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			written, err := service.WriteDocumentType(t.Context(), tt.params)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.params.Name, written.Name)
		})
	}
}

// TestReadAnEarlierTypeWrite pins that a type reads as an earlier write left it, and that a write
// or type nobody made is not found.
func TestReadAnEarlierTypeWrite(t *testing.T) {
	stores := storetest.Open(t)
	service := newService(stores, eventstest.NewQueue(t, stores, documents.NewEventHandler()))
	ctx := t.Context()

	draft, err := service.WriteDocumentType(ctx, documents.WriteDocumentTypeParams{
		Name:    "postmortem",
		Context: "draft",
	})
	require.NoError(t, err)
	_, err = service.WriteDocumentType(ctx, documents.WriteDocumentTypeParams{
		ID:      xptr.Ptr(draft.ID),
		Version: xptr.Ptr(draft.Version),
		Name:    "incident",
		Context: "revised",
	})
	require.NoError(t, err)

	first, err := service.GetDocumentType(ctx, "incident", xptr.Ptr(int32(1)))
	require.NoError(t, err)
	assert.Equal(t, draft.ID, first.ID)
	assert.Equal(t, "postmortem", first.Name)
	assert.Equal(t, "draft", first.Context)
	assert.Equal(t, int32(1), first.Version)

	current, err := service.GetDocumentType(ctx, "incident", nil)
	require.NoError(t, err)
	assert.Equal(t, "revised", current.Context)

	_, err = service.GetDocumentType(ctx, "incident", xptr.Ptr(int32(3)))
	require.ErrorIs(t, err, store.ErrNotFound)
	_, err = service.ListDocumentTypeVersions(ctx, uuid.NewString())
	require.ErrorIs(t, err, store.ErrNotFound)
}
