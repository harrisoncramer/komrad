package cli

import (
	"context"
	"errors"
	"fmt"

	"komrad/config"
	"komrad/internal/postgres"

	"github.com/spf13/cobra"
)

const needsEmbeddedPostgres = "komrad.needs-embedded-postgres"

func doesNeedEmbeddedPostgres(cmd *cobra.Command) bool {
	for current := cmd; current != nil; current = current.Parent() {
		if _, ok := current.Annotations[needsEmbeddedPostgres]; ok {
			return true
		}
	}

	return false
}

// getEmbeddedPostgresDir is where the embedded Postgres lives, and empty when the config turns it off.
func getEmbeddedPostgresDir(env *config.Environment) string {
	if !env.EmbeddedPostgres {
		return ""
	}

	return config.GetEmbeddedPostgresDir(env.StateDir)
}

// requireEmbeddedPostgres refuses a command that needs the database while the embedded Postgres is down.
func requireEmbeddedPostgres(ctx context.Context, cmd *cobra.Command, env *config.Environment) error {
	dir := getEmbeddedPostgresDir(env)
	if dir == "" || !doesNeedEmbeddedPostgres(cmd) || postgres.IsRunning(ctx, dir) {
		return nil
	}

	return fmt.Errorf("the embedded Postgres in %s is not running, run komrad start first", dir)
}

// startEmbeddedPostgres starts the embedded Postgres in dir unless it is running or dir is empty.
func startEmbeddedPostgres(ctx context.Context, dir string) error {
	if dir == "" {
		return nil
	}

	if err := postgres.Start(ctx, dir); err != nil {
		return fmt.Errorf("failed to start the embedded Postgres: %w", err)
	}

	return nil
}

// createOrStartEmbeddedPostgres creates the embedded Postgres in dir when there is none and starts it otherwise,
// reporting whether it created one.
func createOrStartEmbeddedPostgres(ctx context.Context, dir string) (bool, error) {
	err := postgres.CreateCluster(ctx, dir)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, postgres.ErrClusterExists) {
		return false, fmt.Errorf("failed to create the embedded Postgres: %w", err)
	}

	return false, startEmbeddedPostgres(ctx, dir)
}
