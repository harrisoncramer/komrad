package cli

import (
	"github.com/spf13/cobra"
)

// migrateCmd brings the database schema up to date and exits.
//
// Opening the store migrates already, so every command does this on the way in.
// This exists to do it on its own, as a step that can run before anything is
// serving, and to give the result its own exit code rather than folding a schema
// failure into whatever command happened to be first.
func migrateCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:           "migrate",
		Short:         "apply any pending database migrations",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := state.openStore(cmd.Context()); err != nil {
				return err
			}

			// Opening the store migrates it, so reaching here is the success.
			cmd.Println("database schema is up to date")

			return nil
		},
	}
}
