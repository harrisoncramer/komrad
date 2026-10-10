package notes

import (
	"context"

	"komrad/internal/store"
	"komrad/internal/store/notestore"
)

type Handler struct {
	notes *notestore.NoteStore
	db    store.DBTX
}

func (h *Handler) Read(ctx context.Context, id string) (*notestore.Note, error) {
	return h.notes.Get(ctx, id) // want `\(\*komrad/internal/store/notestore.NoteStore\).Get is a store query; komrad/internal/rpc/notes reaches the database through a service`
}

func (h *Handler) Build() *notestore.NoteStore {
	return notestore.NewNoteStore(h.db) // want `komrad/internal/store/notestore.NewNoteStore is a store query`
}

func (h *Handler) Render(notes []notestore.Note) []string {
	return notestore.IDs(notes)
}
