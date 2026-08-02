package cli

import (
	"weave/db"
	"weave/internal/store"

	"github.com/spf13/cobra"
)

// migrateCmd brings both schemas up to date and exits.
//
// Opening the store applies the application's, so every command does that on the way in.
// This exists to do it on its own, before anything is serving, and to give the result its
// own exit code rather than folding a schema failure into whatever command ran first.
func migrateCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:           "migrate",
		Short:         "apply any pending database migrations",
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

			// River's are not on the store's path, since only serve runs the queue, so
			// migrating on purpose has to ask for them.
			if err := db.MigrateRiver(cmd.Context(), pool.GetRawPool()); err != nil {
				return err
			}

			cmd.Println("database schema is up to date")

			return nil
		},
	}
}
