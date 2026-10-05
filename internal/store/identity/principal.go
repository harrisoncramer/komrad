// Package identity stores the principals that may call an engine's api and their bindings to engines.
package identity

import (
	"context"
	"fmt"
	"time"

	"komrad/db"
	"komrad/db/models"
	"komrad/internal/store"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
)

// Kind is whether a principal is a person or a service.
type Kind string

const (
	KindPerson  Kind = Kind(models.IdentityPrincipalKindPerson)
	KindService Kind = Kind(models.IdentityPrincipalKindService)
)

// Provider is the outside party that vouches for a principal.
type Provider string

const (
	providerCloudflareAccess Provider = Provider(models.IdentityProviderCloudflareAccess)
	ProviderGitHub           Provider = Provider(models.IdentityProviderGithub)
	ProviderGoogle           Provider = Provider(models.IdentityProviderGoogle)
)

// Providers is every provider a principal may name.
var Providers = []Provider{providerCloudflareAccess, ProviderGitHub, ProviderGoogle}

// Principal is a person or service that may call an engine's api.
type Principal struct {
	ID         string
	Name       string
	Kind       Kind
	Provider   *Provider
	Subject    *string
	DisabledAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type PrincipalStore struct {
	queries *models.Queries
}

// NewPrincipalStore builds a principal store over the owner's pool or a transaction on it.
func NewPrincipalStore(db store.DBTX) *PrincipalStore {
	return &PrincipalStore{queries: models.New(db)}
}

type CreateParams struct {
	Name     string
	Kind     Kind
	Provider *Provider
	Subject  *string
}

// Create adds a principal, refusing a name or an outside identity another principal holds.
func (s *PrincipalStore) Create(ctx context.Context, params CreateParams) (*Principal, error) {
	var provider models.NullIdentityProvider
	if params.Provider != nil {
		provider = models.NullIdentityProvider{IdentityProvider: models.IdentityProvider(*params.Provider), Valid: true}
	}

	row, err := s.queries.InsertPrincipal(ctx, models.InsertPrincipalParams{
		Name:     params.Name,
		Kind:     models.IdentityPrincipalKind(params.Kind),
		Provider: provider,
		Subject:  params.Subject,
	})
	if db.IsUniqueViolation(err) {
		return nil, s.describeTaken(ctx, params.Name, provider, params.Subject)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create principal %q: %w", params.Name, err)
	}

	created := principalFromRow(row)

	return &created, nil
}

// GetByName returns the principal with that name.
func (s *PrincipalStore) GetByName(ctx context.Context, name string) (*Principal, error) {
	row, err := s.queries.GetPrincipalByName(ctx, name)
	if db.IsNoRows(err) {
		return nil, fmt.Errorf("%w: no principal is named %q", store.ErrNotFound, name)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get principal %q: %w", name, err)
	}

	got := principalFromRow(row)

	return &got, nil
}

// List returns every principal, ordered by name.
func (s *PrincipalStore) List(ctx context.Context) ([]Principal, error) {
	rows, err := s.queries.ListPrincipals(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list principals: %w", err)
	}

	return xslice.Map(rows, principalFromRow), nil
}

// SetDisabled disables or enables the named principal, keeping the time of a disable already in place.
func (s *PrincipalStore) SetDisabled(ctx context.Context, name string, disabled bool) (*Principal, error) {
	row, err := s.queries.SetPrincipalDisabled(ctx, models.SetPrincipalDisabledParams{Name: name, Disabled: disabled})
	if db.IsNoRows(err) {
		return nil, fmt.Errorf("%w: no principal is named %q", store.ErrNotFound, name)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to set whether principal %q is disabled: %w", name, err)
	}

	updated := principalFromRow(row)

	return &updated, nil
}

// Delete removes the named principal and its bindings, returning how many bindings went with it.
func (s *PrincipalStore) Delete(ctx context.Context, name string) (int64, error) {
	row, err := s.queries.DeletePrincipal(ctx, name)
	if db.IsNoRows(err) {
		return 0, fmt.Errorf("%w: no principal is named %q", store.ErrNotFound, name)
	}
	if err != nil {
		return 0, fmt.Errorf("failed to remove principal %q: %w", name, err)
	}

	return row.Bindings, nil
}

func (s *PrincipalStore) describeTaken(ctx context.Context, name string, provider models.NullIdentityProvider, subject *string) error {
	if _, err := s.queries.GetPrincipalByName(ctx, name); err == nil {
		return fmt.Errorf("%w: a principal is already named %q", store.ErrNameTaken, name)
	}

	holder, err := s.queries.GetPrincipalNameByIdentity(ctx, models.GetPrincipalNameByIdentityParams{Provider: provider, Subject: subject})
	if err != nil {
		return fmt.Errorf("%w: the name or identity of principal %q is already taken", store.ErrNameTaken, name)
	}

	return fmt.Errorf("%w: principal %q already holds that identity", store.ErrNameTaken, holder)
}

func principalFromRow(row *models.IdentityPrincipal) Principal {
	var provider *Provider
	if row.Provider.Valid {
		provider = xptr.Ptr(Provider(row.Provider.IdentityProvider))
	}

	return Principal{
		ID:         row.ID,
		Name:       row.Name,
		Kind:       Kind(row.Kind),
		Provider:   provider,
		Subject:    row.Subject,
		DisabledAt: row.DisabledAt,
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
	}
}
