package identity

import (
	"context"
	"fmt"

	"komrad/db"
	"komrad/db/models"
	"komrad/internal/principalrole"
	"komrad/internal/store"
	"komrad/lib/xslice"
)

// Binding is the role one principal holds on one engine.
type Binding struct {
	PrincipalID string
	EngineID    string
	EngineName  string
	Role        principalrole.Role
}

type BindingStore struct {
	queries *models.Queries
}

// NewBindingStore builds a binding store over the owner's pool or a transaction on it.
func NewBindingStore(db store.DBTX) *BindingStore {
	return &BindingStore{queries: models.New(db)}
}

// GetEngineByName returns the engine with that name.
func (s *BindingStore) GetEngineByName(ctx context.Context, name string) (*store.Engine, error) {
	row, err := s.queries.GetEngineByName(ctx, name)
	if db.IsNoRows(err) {
		return nil, fmt.Errorf("%w: no engine is named %q", store.ErrNotFound, name)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get engine %q: %w", name, err)
	}

	return &store.Engine{ID: row.ID, Name: row.Name}, nil
}

type SetParams struct {
	PrincipalID string
	EngineID    string
	Role        principalrole.Role
}

// Set gives the principal the role on the engine, returning the role it replaced or an empty one when it had none.
func (s *BindingStore) Set(ctx context.Context, params SetParams) (principalrole.Role, error) {
	row, err := s.queries.UpsertBinding(ctx, models.UpsertBindingParams{
		PrincipalID: params.PrincipalID,
		EngineID:    params.EngineID,
		Role:        models.IdentityPrincipalRole(params.Role),
	})
	if err != nil {
		return "", fmt.Errorf("failed to bind principal %q to engine %q: %w", params.PrincipalID, params.EngineID, err)
	}

	return principalrole.Role(row.PreviousRole.IdentityPrincipalRole), nil
}

// Delete removes the principal's binding on the engine, if it has one.
func (s *BindingStore) Delete(ctx context.Context, principalID, engineID string) error {
	if err := s.queries.DeleteBinding(ctx, models.DeleteBindingParams{PrincipalID: principalID, EngineID: engineID}); err != nil {
		return fmt.Errorf("failed to unbind principal %q from engine %q: %w", principalID, engineID, err)
	}

	return nil
}

// List returns every binding, ordered by engine name.
func (s *BindingStore) List(ctx context.Context) ([]Binding, error) {
	rows, err := s.queries.ListBindings(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list bindings: %w", err)
	}

	return xslice.Map(rows, func(row *models.ListBindingsRow) Binding {
		return Binding{
			PrincipalID: row.PrincipalID,
			EngineID:    row.EngineID,
			EngineName:  row.EngineName,
			Role:        principalrole.Role(row.Role),
		}
	}), nil
}
