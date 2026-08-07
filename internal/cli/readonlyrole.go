package cli

import (
	"fmt"

	"weave/db"
	"weave/internal/store"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/spf13/cobra"
)

// readonlyRoleCmd provisions the least-privilege login the MCP query tools connect as. It is
// its own command rather than part of the migrations, since a role is a cluster object and
// creating one needs CREATEROLE, a privilege the binary should not have to hold on the path
// every other command takes through the store. The role's name and password are read out of
// the read-only url rather than taken as flags, so the credentials this creates match what
// serve connects with, whether that url was set or derived from the writable one.
func readonlyRoleCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "readonly-role",
		Short: "Create the least-privilege role the MCP query tools connect as",
		Long: "Create the least-privilege role the MCP query tools connect as.\n\n" +
			"The SQL half of the MCP endpoint runs whatever an agent asks it to, so it connects\n" +
			"as a login that can read the application tables and nothing else. That is enforced\n" +
			"by Postgres rather than by inspecting the query. This creates or updates that role\n" +
			"from the user and password in the read-only url, which is WEAVE_READONLY_DATABASE_URL\n" +
			"when it is set and the writable url with the weave_readonly login otherwise, using\n" +
			"the writable url to do the granting.\n\n" +
			"It refuses when the two URLs name the same login, since then the read-only path would\n" +
			"have the owner's privileges. Run it once when setting a database up. Tables a later\n" +
			"migration adds are covered on their own, and running it again is harmless.",
		Args:          cobra.NoArgs,
		Hidden:        true,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Checked before the parse, since Postgres reads an empty string as every
			// default and this would go on to grant read to whoever is running weave.
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
