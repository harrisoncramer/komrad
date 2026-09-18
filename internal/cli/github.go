package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"weave/internal/forge"
	"weave/internal/forge/forges"
	githubapp "weave/internal/github/app"
	"weave/lib/xhttp"

	"github.com/spf13/cobra"
)

const githubTokenTimeout = 20 * time.Second

func githubCmd(state *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "github",
		Short: "Reach GitHub as the engine's own App",
		Long: `Reach GitHub as the engine's own App.

An engine runs agents that write code, so it has to push. Giving it a personal
access token would leave a secret on the machine that works forever.

Instead weave holds a GitHub App's private key and signs a short assertion with it.
GitHub returns a token that stops working after an hour and only reaches the
repositories the App was installed on.

Set the App id, the installation id and the path to the key under ` + "`[github_app]`" + `.`,
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

Run this to check the App is set up. A token starting ghs_ means the key, the App id
and the installation id all agree. Any other answer says which one does not.`,
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
		Long: `Answer git when it asks for a password.

Git runs this and describes what it wants to reach. This answers only for https to
github.com. Anything else gets no answer, so git falls back to whatever else the
account has configured.

Nothing is stored. The token is made when git asks and stops working within the hour.

Set it up with:

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

			query := githubapp.CredentialQuery(string(input))
			if !strings.EqualFold(query["protocol"], "https") || !strings.EqualFold(query["host"], "github.com") {
				return nil
			}

			token, err := githubToken(cmd.Context(), state)
			if err != nil {
				return err
			}

			fmt.Fprint(cmd.OutOrStdout(), githubapp.CredentialReply(token)) //nolint:errcheck // git closing the pipe is not this command failing

			return nil
		},
	}
}

// githubToken is the cached installation token, or a freshly minted one recorded for next time.
func githubToken(ctx context.Context, state *app) (string, error) {
	current, err := githubApp(state)
	if err != nil {
		return "", err
	}

	if !current.IsConfigured() {
		return "", fmt.Errorf("no github app is configured: set app_id, installation_id and " +
			"private_key_path under [github_app] in the config this weave was started with")
	}

	token, err := current.Cache.Fetch(ctx, xhttp.NewClient(githubTokenTimeout), time.Now())
	if err != nil {
		return "", err
	}

	return token.Value, nil
}

// githubApp is the App the config names, with its cache where every command shares it.
func githubApp(state *app) (forge.App, error) {
	return forges.AppFrom(state.githubApp, githubTokenCachePath(state))
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
