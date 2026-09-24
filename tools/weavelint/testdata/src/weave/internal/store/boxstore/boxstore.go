package boxstore

import "context"

type SendParams struct {
	Body string
}

type Box struct {
	ID string
}

type Store struct{}

func (s *Store) Send(_ context.Context, _ SendParams) error {
	return nil
}

func (s *Store) Seed(_ []*Box) {}
