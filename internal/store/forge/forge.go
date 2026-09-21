// Package forge stores the forges weave has been connected to, one row per host.
package forge

import (
	"context"
	"fmt"
	"time"

	"weave/db"
	"weave/db/models"
	"weave/internal/fail"
	"weave/internal/store"
)

// ErrHostTaken says a host already has a connection.
var ErrHostTaken = fail.AlreadyExists("this host is already connected")

// Connection is one forge a machine acts on, and who it acts as there.
type Connection struct {
	ID string
	// Kind is which forge the host runs, github or gitlab.
	Kind string
	// Host is what agents' remotes name, such as github.com.
	Host string
	// APIURL is where an enterprise host answers API calls, empty for the public forge.
	APIURL string
	// Method is how the credential was obtained, token or github_app.
	Method string
	// Login is who the forge said the credential belongs to.
	Login string
	// VerifiedAt is when the forge last confirmed the credential, nil once it refused.
	VerifiedAt *time.Time
	// ThrottledUntil is when the forge said the credential's rate limit resets, nil when it
	// has not said so.
	ThrottledUntil *time.Time
	CreatedAt      time.Time
}

// CreateParams is what a connection is created from.
type CreateParams struct {
	Kind       string
	Host       string
	APIURL     string
	Method     string
	Login      string
	VerifiedAt time.Time
}

// Store reads and writes forge connections.
type Store struct {
	queries *models.Queries
}

// New builds a connection store over a pool or transaction.
func New(db store.DBTX) *Store {
	return &Store{queries: models.New(db)}
}

// Create records a connection, refusing a host that already has one.
func (s *Store) Create(ctx context.Context, params CreateParams) (*Connection, error) {
	verified := params.VerifiedAt
	row, err := s.queries.CreateForgeConnection(ctx, models.CreateForgeConnectionParams{
		Kind:       params.Kind,
		Host:       params.Host,
		ApiUrl:     params.APIURL,
		Method:     params.Method,
		Login:      params.Login,
		VerifiedAt: &verified,
	})
	if db.IsUniqueViolation(err) {
		return nil, ErrHostTaken.Saying("%s is already connected", params.Host)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to connect %s: %w", params.Host, err)
	}

	connection := fromRow(row)

	return &connection, nil
}

// Get is a connection by id, or ErrNotFound.
func (s *Store) Get(ctx context.Context, id string) (*Connection, error) {
	if !store.IsID(id) {
		return nil, store.ErrNotFound.Saying("no connection with id %q", id)
	}

	row, err := s.queries.GetForgeConnection(ctx, id)
	if db.IsNoRows(err) {
		return nil, store.ErrNotFound.Saying("no connection with id %q", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get connection %q: %w", id, err)
	}

	connection := fromRow(row)

	return &connection, nil
}

// GetByHost is the connection for a host, or ErrNotFound.
func (s *Store) GetByHost(ctx context.Context, host string) (*Connection, error) {
	row, err := s.queries.GetForgeConnectionByHost(ctx, host)
	if db.IsNoRows(err) {
		return nil, store.ErrNotFound.Saying("%s is not connected", host)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get the connection for %s: %w", host, err)
	}

	connection := fromRow(row)

	return &connection, nil
}

// List is every connection, by host.
func (s *Store) List(ctx context.Context) ([]Connection, error) {
	rows, err := s.queries.ListForgeConnections(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list connections: %w", err)
	}

	connections := make([]Connection, 0, len(rows))
	for _, row := range rows {
		connections = append(connections, fromRow(row))
	}

	return connections, nil
}

// SetVerifiedAt records when the forge last confirmed the credential, or clears it.
func (s *Store) SetVerifiedAt(ctx context.Context, id string, at *time.Time) error {
	if !store.IsID(id) {
		return store.ErrNotFound.Saying("no connection with id %q", id)
	}

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
func (s *Store) SetThrottledUntil(ctx context.Context, id string, until *time.Time) error {
	if !store.IsID(id) {
		return store.ErrNotFound.Saying("no connection with id %q", id)
	}

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
func (s *Store) Delete(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return store.ErrNotFound.Saying("no connection with id %q", id)
	}

	deleted, err := s.queries.DeleteForgeConnection(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to delete connection %q: %w", id, err)
	}
	if deleted == 0 {
		return store.ErrNotFound.Saying("no connection with id %q", id)
	}

	return nil
}

func fromRow(row *models.ForgeConnection) Connection {
	return Connection{
		ID:             row.ID,
		Kind:           row.Kind,
		Host:           row.Host,
		APIURL:         row.ApiUrl,
		Method:         row.Method,
		Login:          row.Login,
		VerifiedAt:     row.VerifiedAt,
		ThrottledUntil: row.ThrottledUntil,
		CreatedAt:      row.CreatedAt,
	}
}
