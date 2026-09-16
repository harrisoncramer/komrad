package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"weave/config"
	"weave/internal/github"

	"github.com/spf13/cobra"
)

const githubTokenTimeout = 20 * time.Second

func githubCmd(state *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "github",
		Short: "Reach GitHub as the engine's own App",
		Long: `Reach GitHub as the engine's own App.

An engine that writes code has to push it, and the usual ways to let a machine do
that leave a long-lived secret on it. Instead weave holds a GitHub App's private
key, signs a brief assertion with it, and exchanges that for an installation token
that expires in an hour and only reaches the repositories the App was installed on.

Configure it under ` + "`[github]`" + ` with the App id, the installation id, and the path
to the key.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.AddCommand(githubTokenCmd(state), githubCredentialCmd(state))

	return cmd
}

func githubTokenCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "token",
		Short: "Print an installation token, minting one if the cached one is spent",
		Long: `Print an installation token.

This is how to check the App is wired up: a token starting ghs_ means the key, the
App id and the installation id agree. Anything else names which of them does not.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			token, err := githubToken(cmd.Context(), state)
			if err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), token) //nolint:errcheck // a closed stdout is the caller going away

			return nil
		},
	}
}

func githubCredentialCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "credential",
		Short: "Answer git's credential helper protocol with an installation token",
		Long: `Answer git's credential helper protocol.

Git runs this with an action and a description of what it wants to reach. Only
` + "`get`" + ` is answered, and only for https to github.com, so anything else falls
through to whatever else the account has configured. Nothing is stored, because the
token this returns is minted on demand and expires within the hour.

Wire it up with:

    git config --global credential.https://github.com.helper '!weave github credential'`,
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			action := ""
			if len(args) > 0 {
				action = args[0]
			}

			if action != "get" {
				return nil
			}

			input, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), 1<<16))
			if err != nil {
				return fmt.Errorf("failed to read what git asked about: %w", err)
			}

			query := github.CredentialQuery(string(input))
			if !strings.EqualFold(query["protocol"], "https") || !strings.EqualFold(query["host"], "github.com") {
				return nil
			}

			token, err := githubToken(cmd.Context(), state)
			if err != nil {
				return err
			}

			fmt.Fprint(cmd.OutOrStdout(), github.CredentialReply(token)) //nolint:errcheck // git closing the pipe is not this command failing

			return nil
		},
	}
}

// githubToken is the cached installation token, or a freshly minted one recorded for next time.
func githubToken(ctx context.Context, state *app) (string, error) {
	installation, err := github.InstallationIDFromString(state.github.InstallationID)
	if err != nil {
		return "", err
	}

	current := github.App{
		ID:             strings.TrimSpace(state.github.AppID),
		InstallationID: installation,
		PrivateKeyPath: config.ExpandHome(strings.TrimSpace(state.github.PrivateKeyPath)),
	}

	if !current.Configured() {
		return "", fmt.Errorf("no github app is configured: set app_id, installation_id and " +
			"private_key_path under [github] in the config this weave was started with")
	}

	cache := github.Cache{Path: githubTokenCachePath(state)}
	if token := cache.Read(); token.Fresh(time.Now()) {
		return token.Value, nil
	}

	token, err := current.Mint(ctx, &http.Client{Timeout: githubTokenTimeout}, time.Now())
	if err != nil {
		return "", err
	}

	if err := cache.Write(token); err != nil {
		return "", err
	}

	return token.Value, nil
}

func githubTokenCachePath(state *app) string {
	dir := strings.TrimSpace(state.env.CacheDir)
	if dir == "" {
		return ""
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}

	return filepath.Join(dir, "github-token.json")
}
