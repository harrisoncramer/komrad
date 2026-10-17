package cli

import (
	"context"
	"fmt"

	"komrad/config"
	"komrad/internal/postgres"

	"github.com/spf13/cobra"
)

const needsPostgres = "komrad.needs-postgres"

func doesNeedPostgres(cmd *cobra.Command) bool {
	for current := cmd; current != nil; current = current.Parent() {
		if _, ok := current.Annotations[needsPostgres]; ok {
			return true
		}
	}

	return false
}

// postgresDir is where komrad's own Postgres lives, and empty when komrad runs none.
func postgresDir(env *config.Environment) string {
	if !env.RunPostgres {
		return ""
	}

	return postgres.Dir(env.StateDir)
}

// requirePostgres refuses a command that needs the database while the Postgres komrad runs is down.
func requirePostgres(ctx context.Context, cmd *cobra.Command, env *config.Environment) error {
	dir := postgresDir(env)
	if dir == "" || !doesNeedPostgres(cmd) || postgres.IsRunning(ctx, dir) {
		return nil
	}

	return fmt.Errorf("the Postgres komrad runs in %s is not running, run komrad start first", dir)
}

// upPostgres starts the Postgres in dir unless it is running or dir is empty.
func upPostgres(ctx context.Context, dir string) error {
	if dir == "" {
		return nil
	}

	if err := postgres.Up(ctx, dir); err != nil {
		return fmt.Errorf("failed to start komrad's Postgres: %w", err)
	}

	return nil
}
