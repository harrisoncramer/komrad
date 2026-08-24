package cli

import (
	"weave/db"
	"weave/internal/store"

	"github.com/spf13/cobra"
)

func migrateCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply any pending database migrations",
		Long: "Apply any pending database migrations against the configured database.\n\n" +
			"The service migrates its own schema on start, so this is for migrating without\n" +
			"starting one: a fresh database, or a deploy step. It also applies River's queue\n" +
			"migrations, which the service only reaches because it is the process that runs the\n" +
			"queue, so asking on purpose has to ask for both.\n\n" +
			"It is safe to run against a database that is already current; it says so and exits\n" +
			"zero.",
		Args:          cobra.NoArgs,
		Hidden:        true,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			pool, err := store.Open(cmd.Context(), state.env.DatabaseURL)
			if err != nil {
				return err
			}
			defer func() { _ = pool.Close() }()

			if err := db.MigrateRiver(cmd.Context(), pool.GetRawPool()); err != nil {
				return err
			}

			cmd.Println("database schema is up to date")

			return nil
		},
	}
}
