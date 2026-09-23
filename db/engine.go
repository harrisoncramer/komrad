package db

import (
	"context"
	_ "embed"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed engine.sql
var engineSQL string

//go:embed engine_settings.sql
var engineSettingsSQL string

var enginePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,29}$`)

func validateEngineName(name string) error {
	if !enginePattern.MatchString(name) {
		return fmt.Errorf("engine name %q must be 1 to 30 lowercase letters, digits or underscores, starting with a letter", name)
	}

	return nil
}

// EngineLogin is the Postgres login an engine writes with.
func EngineLogin(name string) string {
	return "weave_engine_" + name
}

// EngineReaderLogin is the Postgres login an engine's read-only tools connect as.
func EngineReaderLogin(name string) string {
	return EngineLogin(name) + "_reader"
}

// EngineRiverSchema is the schema an engine keeps its job queue in.
func EngineRiverSchema(name string) string {
	return "river_" + name
}

// EnsureEngineParams names the engine to ensure and the password both its logins take.
type EnsureEngineParams struct {
	Name     string
	Password string
}

// EnsureEngine creates an engine's tenant, logins and River schema, or brings an existing engine's up to date.
func EnsureEngine(ctx context.Context, pool *pgxpool.Pool, params EnsureEngineParams) error {
	if err := validateEngineName(params.Name); err != nil {
		return err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin ensuring engine %s: %w", params.Name, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, engineSettingsSQL, params.Name, params.Password); err != nil {
		return fmt.Errorf("failed to pass engine %s to its setup: %w", params.Name, err)
	}

	if _, err := tx.Exec(ctx, engineSQL); err != nil {
		return fmt.Errorf("failed to ensure engine %s: %w", params.Name, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit engine %s: %w", params.Name, err)
	}

	return nil
}
