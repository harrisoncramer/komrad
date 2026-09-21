package quiet

import (
	"context"

	"weave/db/models"
)

type Store struct {
	queries *models.Queries
}

func (s *Store) SetState(ctx context.Context, id string) error {
	return s.queries.SetAgentStateByID(ctx, id)
}
