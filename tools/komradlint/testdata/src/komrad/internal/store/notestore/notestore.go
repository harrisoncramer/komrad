package notestore

import (
	"context"

	"komrad/internal/store"
)

type Note struct {
	ID string
}

type NoteStore struct {
	db store.DBTX
}

func NewNoteStore(db store.DBTX) *NoteStore {
	return &NoteStore{db: db}
}

func (s *NoteStore) Get(_ context.Context, id string) (*Note, error) {
	return &Note{ID: id}, nil
}

func IDs(notes []Note) []string {
	ids := make([]string, 0, len(notes))
	for _, note := range notes {
		ids = append(ids, note.ID)
	}

	return ids
}
