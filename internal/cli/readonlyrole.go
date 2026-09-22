package cli

import (
	"fmt"

	"weave/db"
	"weave/internal/store"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/spf13/cobra"
)

func readonlyRoleCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "readonly-role",
		Short: "Create the least-privilege role the MCP query tools connect as",
		Long: `Create the least-privilege role the MCP query tools connect as.

The SQL half of the MCP endpoint runs whatever an agent asks it to, so it connects
as a login that can read the application tables and nothing else. That is enforced
by Postgres rather than by inspecting the query. This creates or updates that role
from the user and password in the read-only url, which is WEAVE_READONLY_DATABASE_URL
when it is set and the writable url with the weave_readonly login otherwise, using
the writable url to do the granting.

It refuses when the two URLs name the same login, since then the read-only path would
have the owner's privileges. Run it once when setting a database up. Tables a later
migration adds are covered on their own, and running it again is harmless.`,
		Args:          cobra.NoArgs,
		Hidden:        true,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if state.env.ReadOnlyDatabaseURL == "" {
				return store.ErrNoReadOnlyURL
			}

			if state.env.ReadOnlyDatabaseURL == state.env.DatabaseURL {
				return fmt.Errorf("WEAVE_READONLY_DATABASE_URL names the same login as DATABASE_URL")
			}

			readOnly, err := pgconn.ParseConfig(state.env.ReadOnlyDatabaseURL)
			if err != nil {
				return fmt.Errorf("failed to parse the read-only database url: %w", err)
			}
			if readOnly.User == "" {
				return fmt.Errorf("the read-only database url names no user")
			}

			pool, err := store.Open(cmd.Context(), state.env.DatabaseURL)
			if err != nil {
				return err
			}
			defer func() { _ = pool.Close() }()

			if err := db.GrantReadOnlyRole(
				cmd.Context(), pool.GetRawPool(), readOnly.User, readOnly.Password); err != nil {
				return err
			}

			cmd.Printf("%s may now read the application tables and nothing else\n", readOnly.User)

			return nil
		},
	}
}
