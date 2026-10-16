package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"komrad/internal/browser"
	"komrad/internal/engine"
	"komrad/internal/logging"

	"github.com/spf13/cobra"
)

const maxReason = 1024

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
sign-in ran out.

Login also writes an SSH host for the engine under ~/.ssh/komrad, which carries SSH over the same
sign-in. The panel's edit keys open the engine's files through that host, and so can any editor
that reaches other machines over SSH once ~/.ssh/config includes it.`,
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

			if err := state.setUpSSH(cmd.Context()); err != nil {
				cmd.PrintErrf("Editors cannot reach the engine's files: %v\n", err)

				return nil
			}

			if !isSSHIncluded() {
				cmd.PrintErrf("For editors besides the panel's to reach the engine, add this line to the top of ~/.ssh/config:\n\n  %s\n\n", sshIncludeLine)
			}

			return nil
		},
	}
}

func logoutCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Sign out of an engine reached at its url",
		Long: `Sign out of an engine reached at its url, dropping the sign-in this machine keeps for it.

The sign-in leaves the keychain and the private file under the state directory, so the next
command asks you to run komrad login. A panel open on this machine for the engine starts over at
its sign-in screen.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if state.env.URL == "" {
				return errNoEngineURL
			}

			if err := engine.NewSignInKeeper(state.env.StateDir).DeleteSignIn(state.env.URL); err != nil {
				return err
			}

			if err := restartPanel(cmd.Context(), state); err != nil {
				return err
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Signed out of %s\n", state.env.URL); err != nil {
				return fmt.Errorf("failed to print the sign-out: %w", err)
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

type panelSignIn struct {
	url       string
	transport http.RoundTripper
	refresher *engine.TokenRefresher
	keeper    *engine.SignInKeeper
	opener    browser.Opener
	listen    string
	setUpSSH  func(ctx context.Context) error
}

// CheckAccess asks the engine whether the token this machine holds names someone granted access.
func (s *panelSignIn) CheckAccess(ctx context.Context) error {
	res, err := healthCheck(ctx, s.transport, s.url)
	if err != nil {
		if errors.Is(err, engine.ErrSignInNeeded) || errors.Is(err, engine.ErrProviderUnreachable) {
			var failed *url.Error
			if errors.As(err, &failed) {
				return failed.Err
			}

			return err
		}

		return fmt.Errorf("failed to reach the engine at %s: %w", s.url, err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode == http.StatusOK {
		return nil
	}

	reason := readReason(res.Body)

	switch res.StatusCode {
	case http.StatusUnauthorized:
		return engine.ErrSignInNeeded.Saying("the engine at %s did not take this sign-in: %s", s.url, reason)
	case http.StatusForbidden:
		return engine.ErrAccessDenied.Saying("the engine at %s turned this account away, so ask its owner: %s", s.url, reason)
	default:
		return fmt.Errorf("failed to check access to the engine at %s, which answered %s: %s", s.url, res.Status, reason)
	}
}

// Login signs the person in through the engine's provider in their browser, and drops the
// sign-in the panel held so its next request carries the new one.
func (s *panelSignIn) Login(ctx context.Context, prompt func(authURL string)) error {
	defer s.refresher.Forget()

	if _, err := engine.Login(ctx, engine.LoginParams{
		URL:    s.url,
		Keeper: s.keeper,
		Opener: s.opener,
		Listen: s.listen,
		Prompt: prompt,
	}); err != nil {
		return err
	}

	if err := s.setUpSSH(ctx); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "editors cannot reach the engine's files", logging.Err(err)...)
	}

	return nil
}

func readReason(body io.Reader) string {
	raw, err := io.ReadAll(io.LimitReader(body, maxReason))
	if err != nil {
		return err.Error()
	}

	var refusal struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &refusal) == nil && refusal.Message != "" {
		return refusal.Message
	}

	return strings.TrimSpace(string(raw))
}
