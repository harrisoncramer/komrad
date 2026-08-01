package cli

import (
	"fmt"

	"weave/db"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/spf13/cobra"
)

// readonlyRoleCmd provisions the least-privilege login the MCP query tools connect as.
//
// It is its own command rather than part of the migrations because a role is a cluster
// object and creating one needs CREATEROLE, which is a privilege the binary should not
// have to hold on the path every other command takes through the store.
//
// The role's name and password are read out of WEAVE_READONLY_DATABASE_URL rather than
// taken as flags, so the credentials this creates and the credentials serve connects with
// are the same string in one place.
func readonlyRoleCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:           "readonly-role",
		Short:         "create the least-privilege role the MCP query tools connect as",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// An unset WEAVE_READONLY_DATABASE_URL falls back to the writable url, which
			// would mean granting the application's own role SELECT on its own tables and
			// leaving the endpoint exactly as exposed as it was.
			if state.env.ReadOnlyDatabaseURL == state.env.DatabaseURL {
				return fmt.Errorf(
					"WEAVE_READONLY_DATABASE_URL is unset, so there is no separate role to create; " +
						"set it to a url naming the login you want, then run this again")
			}

			readOnly, err := pgconn.ParseConfig(state.env.ReadOnlyDatabaseURL)
			if err != nil {
				return fmt.Errorf("failed to parse the read-only database url: %w", err)
			}
			if readOnly.User == "" {
				return fmt.Errorf("the read-only database url names no user")
			}

			if err := state.openStore(cmd.Context()); err != nil {
				return err
			}

			// Granted through the writable pool, since the role being created is by
			// definition not one that could create itself.
			if err := db.GrantReadOnlyRole(
				cmd.Context(), state.pool.Raw(), readOnly.User, readOnly.Password); err != nil {
				return err
			}

			cmd.Printf("%s may now read the application tables and nothing else\n", readOnly.User)

			return nil
		},
	}
}
