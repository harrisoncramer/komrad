package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"

	"komrad/config"
	"komrad/db"
	"komrad/internal/store"

	"github.com/spf13/cobra"
)

const enginePasswordVariable = "KOMRAD_ENGINE_PASSWORD"

func engineCmd(state *app) *cobra.Command {
	engine := &cobra.Command{
		Use:         "engine",
		Annotations: map[string]string{needsPostgres: ""},
		Short:       "Set up the engines that share a database, and who may use them",
		Hidden:      true,
	}

	engine.AddCommand(&cobra.Command{
		Use:   "ensure NAME",
		Short: "Create an engine's identity and logins, or reset their password",
		Long: `Create an engine's identity and logins, or reset their password.

An engine is one komrad server and what it owns. Everything it writes belongs to it, and
Postgres keeps every other engine's rows out of its reach, because it connects as a login
of its own that the database maps to its identity. This creates that identity if no engine
has the name yet, a login for it and a read-only login for its MCP query tools, and the
schema its job queue lives in, connecting as owner_database_url.

Both logins take the password in KOMRAD_ENGINE_PASSWORD, or a fresh one when that is unset,
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

			engineURL, err := ensureEngine(cmd.Context(), state.env.OwnerDatabaseURL, name, password)
			if err != nil {
				return err
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "[connection]\ndatabase_url = %q\n", engineURL); err != nil {
				return fmt.Errorf("failed to print the engine's database url: %w", err)
			}

			return nil
		},
	})

	engine.AddCommand(&cobra.Command{
		Use:   "grant NAME EMAIL",
		Short: "Let a person into an engine by their email",
		Long: `Let the person with an email into an engine, connecting as owner_database_url.

The first time they sign in with a verified email, the engine records the permanent id their
identity provider gives that account, and from then on it lets in that account alone. Anyone
granted access can do everything the panel does, including opening a shell on the engine's
machine, so grant it only to people you would give that machine. Granting an email again
changes nothing.`,
		Args:          cobra.ExactArgs(2),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := store.GrantAccess(cmd.Context(), store.GrantAccessParams{
				OwnerURL: state.env.OwnerDatabaseURL,
				Engine:   args[0],
				Email:    args[1],
			}); err != nil {
				return err
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "granted %s access to engine %s\n", args[1], args[0]); err != nil {
				return fmt.Errorf("failed to print the grant: %w", err)
			}

			return nil
		},
	})

	engine.AddCommand(&cobra.Command{
		Use:   "revoke NAME EMAIL",
		Short: "Turn a person away from an engine",
		Long: `Turn the person with an email away from an engine, connecting as owner_database_url.

This removes the grant and the account their sign-in recorded, so letting them back in takes a
fresh grant and a fresh sign-in.`,
		Args:          cobra.ExactArgs(2),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := store.RevokeAccess(cmd.Context(), store.RevokeAccessParams{
				OwnerURL: state.env.OwnerDatabaseURL,
				Engine:   args[0],
				Email:    args[1],
			}); err != nil {
				return err
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "revoked %s's access to engine %s\n", args[1], args[0]); err != nil {
				return fmt.Errorf("failed to print the revoke: %w", err)
			}

			return nil
		},
	})

	return engine
}

// ensureEngine creates the engine name in the database ownerURL owns, or resets its logins to
// password, and returns the url the engine connects with.
func ensureEngine(ctx context.Context, ownerURL, name, password string) (string, error) {
	if err := store.EnsureEngine(ctx, ownerURL, db.EnsureEngineParams{
		Name:     name,
		Password: password,
	}); err != nil {
		return "", err
	}

	return withLogin(ownerURL, db.EngineLogin(name), password)
}

// joinEngine migrates the database ownerURL owns and ensures the engine setup makes there.
func joinEngine(ctx context.Context, ownerURL string) (string, error) {
	if err := store.Migrate(ctx, ownerURL); err != nil {
		return "", err
	}

	password, err := generatePassword()
	if err != nil {
		return "", err
	}

	return ensureEngine(ctx, ownerURL, setupEngine, password)
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
