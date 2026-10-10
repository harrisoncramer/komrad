package notes

import (
	"context"

	"komrad/internal/store/notestore"
)

type Service struct {
	notes *notestore.NoteStore
}

func (s *Service) Read(ctx context.Context, id string) (*notestore.Note, error) {
	return s.notes.Get(ctx, id)
}
