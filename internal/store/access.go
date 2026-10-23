package store

import (
	"context"
	"fmt"
	"net/mail"

	"komrad/db"
	"komrad/db/models"
	"komrad/internal/fail"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotAnEmail is what changing access returns for anything but a bare email address.
var ErrNotAnEmail = fail.InvalidArgument("not an email address")

// GrantAccessParams names the database owner, the engine, and the email to let into it.
type GrantAccessParams struct {
	OwnerURL string
	Engine   string
	Email    string
}

// GrantAccess lets the person with an email into an engine, connecting as the database's owner.
func GrantAccess(ctx context.Context, params GrantAccessParams) error {
	if err := requireEmail(params.Email); err != nil {
		return err
	}

	pool, err := openMigratedOwner(ctx, params.OwnerURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	queries := models.New(pool)

	engineID, err := getEngineID(ctx, queries, params.Engine)
	if err != nil {
		return err
	}

	if err := queries.GrantEngineAccess(ctx, models.GrantEngineAccessParams{EngineID: engineID, Email: params.Email}); err != nil {
		return fmt.Errorf("failed to grant %s access to engine %s: %w", params.Email, params.Engine, err)
	}

	return nil
}

// RevokeAccessParams names the database owner, the engine, and the email to turn away from it.
type RevokeAccessParams struct {
	OwnerURL string
	Engine   string
	Email    string
}

// RevokeAccess turns the person with an email away from an engine, along with the account their sign-in recorded.
func RevokeAccess(ctx context.Context, params RevokeAccessParams) error {
	if err := requireEmail(params.Email); err != nil {
		return err
	}

	pool, err := openMigratedOwner(ctx, params.OwnerURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	queries := models.New(pool)

	engineID, err := getEngineID(ctx, queries, params.Engine)
	if err != nil {
		return err
	}

	revoked, err := queries.RevokeEngineAccess(ctx, models.RevokeEngineAccessParams{EngineID: engineID, Email: params.Email})
	if err != nil {
		return fmt.Errorf("failed to revoke %s's access to engine %s: %w", params.Email, params.Engine, err)
	}

	if revoked == 0 {
		return ErrNotFound.Saying("%s has no access to engine %s", params.Email, params.Engine)
	}

	return nil
}

func requireEmail(email string) error {
	address, err := mail.ParseAddress(email)
	if err != nil || address.Name != "" || address.Address != email {
		return ErrNotAnEmail.Saying("%q is not an email address", email)
	}

	return nil
}

func getEngineID(ctx context.Context, queries *models.Queries, name string) (uuid.UUID, error) {
	engineID, err := queries.GetEngineIDByName(ctx, name)
	if db.IsNoRows(err) {
		return uuid.Nil, ErrNotFound.Saying("no engine is named %s", name)
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to find engine %s: %w", name, err)
	}

	return engineID, nil
}

// AccessStore answers, as the engine, whether a signed-in caller has access to it.
type AccessStore struct {
	queries *models.Queries
}

// NewAccessStore builds an access store over db, which is either the pool or a transaction.
func NewAccessStore(db DBTX) *AccessStore {
	return &AccessStore{queries: models.New(db)}
}

// ClaimAccessParams names the verified email a caller signed in with, and who the provider says they are.
type ClaimAccessParams struct {
	Email   string
	Issuer  string
	Subject string
}

// ClaimAccess records who the provider says holds an email granted access, on the first sign-in with it.
func (s *AccessStore) ClaimAccess(ctx context.Context, params ClaimAccessParams) (bool, error) {
	claimed, err := s.queries.ClaimEngineAccess(ctx, models.ClaimEngineAccessParams{
		Email:   params.Email,
		Issuer:  params.Issuer,
		Subject: params.Subject,
	})
	if err != nil {
		return false, fmt.Errorf("failed to claim access for %s: %w", params.Email, err)
	}

	return claimed, nil
}

// IsAccessGranted reports whether the caller a provider vouches for has access to this engine.
func (s *AccessStore) IsAccessGranted(ctx context.Context, issuer, subject string) (bool, error) {
	granted, err := s.queries.IsEngineAccessGranted(ctx, models.IsEngineAccessGrantedParams{
		Issuer:  issuer,
		Subject: subject,
	})
	if err != nil {
		return false, fmt.Errorf("failed to read whether %s at %s has access: %w", subject, issuer, err)
	}

	return granted, nil
}

// grantStore answers, as the database's owner, whether a person holds the grant on an engine.
type grantStore struct {
	pool    *pgxpool.Pool
	queries *models.Queries
}

// OpenGrantStore connects to the database at ownerURL as its owner.
func OpenGrantStore(ctx context.Context, ownerURL string) (*grantStore, error) {
	pool, err := openMigratedOwner(ctx, ownerURL)
	if err != nil {
		return nil, err
	}

	return &grantStore{pool: pool, queries: models.New(pool)}, nil
}

// Close releases the connection pool.
func (s *grantStore) Close() {
	s.pool.Close()
}

// IsGrantedParams names the engine and the signed-in person asking for it.
type IsGrantedParams struct {
	Engine        string
	Email         string
	EmailVerified bool
	Issuer        string
	Subject       string
}

// IsGranted reports whether the person's sign-in matches the engine's grant, by the account a
// sign-in recorded or by a verified email on a grant nobody has signed in with yet.
func (s *grantStore) IsGranted(ctx context.Context, params IsGrantedParams) (bool, error) {
	granted, err := s.queries.IsEngineGranted(ctx, models.IsEngineGrantedParams{
		Engine:        params.Engine,
		Issuer:        params.Issuer,
		Subject:       params.Subject,
		EmailVerified: params.EmailVerified,
		Email:         params.Email,
	})
	if err != nil {
		return false, fmt.Errorf("failed to read whether %s holds the grant on engine %s: %w", params.Subject, params.Engine, err)
	}

	return granted, nil
}
