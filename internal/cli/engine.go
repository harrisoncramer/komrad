package cli

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"

	"komrade/config"
	"komrade/db"
	"komrade/internal/store"

	"github.com/spf13/cobra"
)

const enginePasswordVariable = "KOMRADE_ENGINE_PASSWORD"

func engineCmd(state *app) *cobra.Command {
	engine := &cobra.Command{
		Use:    "engine",
		Short:  "Set up the engines that share a database",
		Hidden: true,
	}

	engine.AddCommand(&cobra.Command{
		Use:   "ensure NAME",
		Short: "Create an engine's identity and logins, or reset their password",
		Long: `Create an engine's identity and logins, or reset their password.

An engine is one komrade service on one machine. Everything it writes belongs to it, and
Postgres keeps every other engine's rows out of its reach, because it connects as a login
of its own that the database maps to its identity. This creates that identity if no engine
has the name yet, a login for it and a read-only login for its MCP query tools, and the
schema its job queue lives in, connecting as owner_database_url.

Both logins take the password in KOMRADE_ENGINE_PASSWORD, or a fresh one when that is unset,
and running it again resets them to it. It prints the database_url the engine connects with.
Naming an engine that already exists, such as the default one every row written before
engines had logins belongs to, hands it new logins and keeps its rows.`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			password, ok := config.LookupEnv(enginePasswordVariable)
			if !ok || password == "" {
				generated, err := generatePassword()
				if err != nil {
					return err
				}
				password = generated
			}

			if err := store.EnsureEngine(cmd.Context(), state.env.OwnerDatabaseURL, db.EnsureEngineParams{
				Name:     name,
				Password: password,
			}); err != nil {
				return err
			}

			engineURL, err := withLogin(state.env.OwnerDatabaseURL, db.EngineLogin(name), password)
			if err != nil {
				return err
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "[connection]\ndatabase_url = %q\n", engineURL); err != nil {
				return fmt.Errorf("failed to print the engine's database url: %w", err)
			}

			return nil
		},
	})

	return engine
}

func generatePassword() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed to generate a password: %w", err)
	}

	return hex.EncodeToString(buf), nil
}

func withLogin(raw, user, password string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("failed to parse the owner database url: %w", err)
	}
	parsed.User = url.UserPassword(user, password)

	return parsed.String(), nil
}
