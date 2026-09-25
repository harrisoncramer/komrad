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

func (s *Store) Get(_ context.Context, _ string) (*Box, error) {
	return nil, nil
}

func (s *Store) Validate() error {
	return nil
}
