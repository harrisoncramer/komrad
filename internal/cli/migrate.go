package cli

import (
	"weave/internal/store"

	"github.com/spf13/cobra"
)

func migrateCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply any pending database migrations",
		Long: `Apply any pending database migrations, connecting as the login that owns the schema.

The service never migrates: an engine connects as a login that cannot change the schema,
and it refuses to start against a database whose migrations do not match its build. So
every upgrade runs this first, with owner_database_url (or WEAVE_OWNER_DATABASE_URL)
naming the owner, which falls back to database_url on a database no engine has joined.

It refuses while another weave is connected and the build holds migrations the database
has not seen, since those engines would stop matching. It is safe to run against a
database that is already current; it says so and exits zero.`,
		Args:          cobra.NoArgs,
		Hidden:        true,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := store.Migrate(cmd.Context(), state.env.OwnerDatabaseURL); err != nil {
				return err
			}

			cmd.Println("database schema is up to date")

			return nil
		},
	}
}
