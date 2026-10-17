package cli

import (
	"context"
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

// embeddedPostgresDir is where the embedded Postgres lives, and empty when the config turns it off.
func embeddedPostgresDir(env *config.Environment) string {
	if !env.EmbeddedPostgres {
		return ""
	}

	return postgres.GetDir(env.StateDir)
}

// requireEmbeddedPostgres refuses a command that needs the database while the embedded Postgres is down.
func requireEmbeddedPostgres(ctx context.Context, cmd *cobra.Command, env *config.Environment) error {
	dir := embeddedPostgresDir(env)
	if dir == "" || !doesNeedEmbeddedPostgres(cmd) || postgres.IsRunning(ctx, dir) {
		return nil
	}

	return fmt.Errorf("the embedded Postgres in %s is not running, run komrad start first", dir)
}

// startEmbeddedPostgres starts the Postgres in dir unless it is running or dir is empty.
func startEmbeddedPostgres(ctx context.Context, dir string) error {
	if dir == "" {
		return nil
	}

	if err := postgres.Start(ctx, dir); err != nil {
		return fmt.Errorf("failed to start the embedded Postgres: %w", err)
	}

	return nil
}
