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

// requirePostgres refuses a command that needs the database while the Postgres komrad runs is down.
func requirePostgres(ctx context.Context, cmd *cobra.Command, env *config.Environment) error {
	if !env.RunPostgres || !doesNeedPostgres(cmd) {
		return nil
	}

	dir := postgres.Dir(env.StateDir)
	if postgres.IsRunning(ctx, dir) {
		return nil
	}

	return fmt.Errorf("the Postgres komrad runs in %s is not running, run komrad start first", dir)
}
