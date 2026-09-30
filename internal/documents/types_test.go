package documents_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"komrad/internal/documents"
	"komrad/internal/events/eventstest"
	documentstore "komrad/internal/store/document"
	"komrad/internal/storetest"
	"komrad/lib/xptr"
)

// TestGetDocumentType pins that an unknown name is refused with the names that exist.
func TestGetDocumentType(t *testing.T) {
	stores := storetest.Open(t)
	stores.SeedDocumentTypes(t, "notes")
	service := newService(stores, eventstest.NewQueue(t, stores, documents.NewEventHandler()))

	got, err := service.GetDocumentType(t.Context(), "notes")
	require.NoError(t, err)
	assert.Equal(t, "notes", got.Name)

	_, err = service.GetDocumentType(t.Context(), "postmortem")
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
			name:    "a rewrite with no version is refused",
			params:  documents.WriteDocumentTypeParams{ID: &created.ID, Name: "postmortem", Context: "Again."},
			wantErr: documents.ErrNoTypeVersion,
		},
		{
			name:   "a rewrite from the version read is taken",
			params: documents.WriteDocumentTypeParams{ID: &created.ID, Version: xptr.Ptr(int32(1)), Name: "incident", Context: "What broke."},
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
