// Package tenant stores the owners every other row is stamped with.
package tenant

import (
	"context"
	"fmt"

	"weave/db"
	"weave/db/models"
	"weave/internal/store"
)

type TenantStore struct {
	queries *models.Queries
}

// NewTenantStore builds a tenant store over a pool or transaction.
func NewTenantStore(db store.DBTX) *TenantStore {
	return &TenantStore{queries: models.New(db)}
}

// ListOtherHosts names every machine but this one with a weave connected to the database.
func (s *TenantStore) ListOtherHosts(ctx context.Context) ([]string, error) {
	hosts, err := s.queries.ListOtherWeaveHosts(ctx, db.AppHost())
	if err != nil {
		return nil, fmt.Errorf("failed to list the other hosts on the database: %w", err)
	}

	return hosts, nil
}
