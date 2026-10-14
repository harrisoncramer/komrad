package cli

import (
	"errors"
	"fmt"

	"komrad/internal/browser"
	"komrad/internal/engine"

	"github.com/spf13/cobra"
)

var errNoEngineURL = errors.New("this engine is not reached at a url, so there is nothing to sign in to; name one with --engine")

func loginCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Sign in to an engine reached at its url",
		Long: `Sign in to an engine reached at its url, through the identity provider it trusts.

This asks the engine which provider that is, opens the provider's sign-in page in your browser,
and waits on 127.0.0.1:8085 for the provider to send the browser back. The sign-in is kept in the
keychain, or in a private file under the state directory where there is no keychain, and every
command after this one sends it and renews it on its own. Run it again when a command says the
sign-in ran out.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if state.env.URL == "" {
				return errNoEngineURL
			}

			signIn, err := engine.Login(cmd.Context(), engine.LoginParams{
				URL:    state.env.URL,
				Keeper: engine.NewSignInKeeper(state.env.StateDir),
				Opener: browser.NewDesktop(),
				Listen: engine.CallbackAddr,
				Prompt: func(authURL string) {
					cmd.PrintErrf("Opening your browser to sign in. If it does not open, visit:\n\n  %s\n\n", authURL)
				},
			})
			if err != nil {
				return err
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Signed in to %s as %s (%s at %s)\n",
				state.env.URL, signIn.Email, signIn.Subject, signIn.Provider.Issuer); err != nil {
				return fmt.Errorf("failed to print the sign-in: %w", err)
			}

			return nil
		},
	}
}

func tokenCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "token",
		Short: "Print an ID token for an engine reached at its url",
		Long: `Print an ID token for an engine reached at its url, renewing the sign-in when it is near its end.

The token is what every command sends the engine, so a script can call the engine's api with it
as a bearer token.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if state.env.URL == "" {
				return errNoEngineURL
			}

			token, err := state.getTokenRefresher().GetToken(cmd.Context())
			if err != nil {
				return err
			}

			if _, err := fmt.Fprintln(cmd.OutOrStdout(), token); err != nil {
				return fmt.Errorf("failed to print the token: %w", err)
			}

			return nil
		},
	}
}
