// Package forge stores the forges komrad has been connected to, one row per host.
package forge

import (
	"context"
	"fmt"
	"time"

	"komrad/db"
	"komrad/db/models"
	"komrad/internal/fail"
	"komrad/internal/store"
	"komrad/lib/protoenum"
	forgev1 "komrad/proto/gen/forge/v1"

	"github.com/google/uuid"
)

// ErrHostTaken says a host already has a connection.
var ErrHostTaken = fail.AlreadyExists("this host is already connected")

// Connection is one forge a machine acts on, and who it acts as there.
type Connection struct {
	ID uuid.UUID
	// Kind is which forge the host runs.
	Kind forgev1.Kind
	// Host is what agents' remotes name, such as github.com.
	Host string
	// APIURL is where an enterprise host answers API calls, empty for the public forge.
	APIURL string
	// Method is how the credential was obtained.
	Method forgev1.CredentialMethod
	// Login is who the forge said the credential belongs to.
	Login string
	// VerifiedAt is when the forge last confirmed the credential, nil once it refused.
	VerifiedAt *time.Time
	// ThrottledUntil is when the forge said the credential's rate limit resets, nil when it
	// has not said so.
	ThrottledUntil *time.Time
	CreatedAt      time.Time
}

// ForgeStore reads and writes forge connections.
type ForgeStore struct {
	queries *models.Queries
}

// NewForgeStore builds a connection store over a pool or transaction.
func NewForgeStore(db store.DBTX) *ForgeStore {
	return &ForgeStore{queries: models.New(db)}
}

// CreateParams holds what creates a connection.
type CreateParams struct {
	Kind       forgev1.Kind
	Host       string
	APIURL     string
	Method     forgev1.CredentialMethod
	Login      string
	VerifiedAt time.Time
}

// Create records a connection, refusing a host that already has one.
func (s *ForgeStore) Create(ctx context.Context, params CreateParams) (*Connection, error) {
	verified := params.VerifiedAt
	row, err := s.queries.CreateForgeConnection(ctx, models.CreateForgeConnectionParams{
		Kind:       protoenum.Name(params.Kind),
		Host:       params.Host,
		ApiUrl:     params.APIURL,
		Method:     protoenum.Name(params.Method),
		Login:      params.Login,
		VerifiedAt: &verified,
	})
	if db.IsUniqueViolation(err) {
		return nil, ErrHostTaken.Saying("%s is already connected", params.Host)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to connect %s: %w", params.Host, err)
	}

	return fromRow(row)
}

// Get is a connection by id, or ErrNotFound.
func (s *ForgeStore) Get(ctx context.Context, id uuid.UUID) (*Connection, error) {
	row, err := s.queries.GetForgeConnection(ctx, id)
	if db.IsNoRows(err) {
		return nil, store.ErrNotFound.Saying("no connection with id %q", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get connection %q: %w", id, err)
	}

	return fromRow(row)
}

// GetByHost is the connection for a host, or ErrNotFound.
func (s *ForgeStore) GetByHost(ctx context.Context, host string) (*Connection, error) {
	row, err := s.queries.GetForgeConnectionByHost(ctx, host)
	if db.IsNoRows(err) {
		return nil, store.ErrNotFound.Saying("%s is not connected", host)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get the connection for %s: %w", host, err)
	}

	return fromRow(row)
}

// List is every connection, by host.
func (s *ForgeStore) List(ctx context.Context) ([]Connection, error) {
	rows, err := s.queries.ListForgeConnections(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list connections: %w", err)
	}

	return fromRows(rows)
}

// SetVerifiedAt records when the forge last confirmed the credential, or clears it.
func (s *ForgeStore) SetVerifiedAt(ctx context.Context, id uuid.UUID, at *time.Time) error {
	updated, err := s.queries.SetForgeConnectionVerifiedAt(ctx, models.SetForgeConnectionVerifiedAtParams{ID: id, VerifiedAt: at})
	if err != nil {
		return fmt.Errorf("failed to set verified_at on connection %q: %w", id, err)
	}
	if updated == 0 {
		return store.ErrNotFound.Saying("no connection with id %q", id)
	}

	return nil
}

// SetThrottledUntil records when the forge said the credential's rate limit resets, or clears it.
func (s *ForgeStore) SetThrottledUntil(ctx context.Context, id uuid.UUID, until *time.Time) error {
	updated, err := s.queries.SetForgeConnectionThrottledUntil(ctx, models.SetForgeConnectionThrottledUntilParams{ID: id, ThrottledUntil: until})
	if err != nil {
		return fmt.Errorf("failed to set throttled_until on connection %q: %w", id, err)
	}
	if updated == 0 {
		return store.ErrNotFound.Saying("no connection with id %q", id)
	}

	return nil
}

// Delete removes a connection, or ErrNotFound.
func (s *ForgeStore) Delete(ctx context.Context, id uuid.UUID) error {
	deleted, err := s.queries.DeleteForgeConnection(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to delete connection %q: %w", id, err)
	}
	if deleted == 0 {
		return store.ErrNotFound.Saying("no connection with id %q", id)
	}

	return nil
}

func fromRows(rows []*models.ForgeConnection) ([]Connection, error) {
	connections := make([]Connection, len(rows))
	for i, row := range rows {
		connection, err := fromRow(row)
		if err != nil {
			return nil, err
		}
		connections[i] = *connection
	}

	return connections, nil
}

func fromRow(row *models.ForgeConnection) (*Connection, error) {
	kind, err := protoenum.Parse[forgev1.Kind](row.Kind)
	if err != nil {
		return nil, fmt.Errorf("failed to read the connection for %s: %w", row.Host, err)
	}
	method, err := protoenum.Parse[forgev1.CredentialMethod](row.Method)
	if err != nil {
		return nil, fmt.Errorf("failed to read the connection for %s: %w", row.Host, err)
	}

	return &Connection{
		ID:             row.ID,
		Kind:           kind,
		Host:           row.Host,
		APIURL:         row.ApiUrl,
		Method:         method,
		Login:          row.Login,
		VerifiedAt:     row.VerifiedAt,
		ThrottledUntil: row.ThrottledUntil,
		CreatedAt:      row.CreatedAt,
	}, nil
}
