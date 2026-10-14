package store

import (
	"context"
	"fmt"
	"net/mail"

	"komrad/db"
	"komrad/db/models"
	"komrad/internal/fail"
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

func getEngineID(ctx context.Context, queries *models.Queries, name string) (string, error) {
	engineID, err := queries.GetEngineIDByName(ctx, name)
	if db.IsNoRows(err) {
		return "", ErrNotFound.Saying("no engine is named %s", name)
	}
	if err != nil {
		return "", fmt.Errorf("failed to find engine %s: %w", name, err)
	}

	return engineID, nil
}
