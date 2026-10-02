package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"komrad/internal/fail"
	"komrad/internal/forge"
	"komrad/internal/git"
	githubapp "komrad/internal/github/app"
	forgev1 "komrad/proto/gen/forge/v1"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/emptypb"
)

// errNoGitLabConnection says komrad holds no GitLab token for a host.
var errNoGitLabConnection = fail.Precondition("no gitlab forge with a token is connected for this host")

func gitlabCmd(state *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gitlab",
		Short: "Reach GitLab with the token its forge connection holds",
		Long: `Reach GitLab with the token its forge connection holds.

GitLab has no App to mint short-lived tokens from, so komrad hands out the token the
host was connected with through komrad forge connect. The glab stand-in komrad puts
ahead of an agent's PATH and git's credential helper both read it here, so neither
keeps a copy of its own.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.AddCommand(gitlabEnvCmd(state), gitlabCredentialCmd(state))

	return cmd
}

func gitlabEnvCmd(state *app) *cobra.Command {
	var (
		checkout     string
		ifConfigured bool
	)

	cmd := &cobra.Command{
		Use:   "env",
		Short: "Print the variables glab reaches a checkout's GitLab with",
		Long: `Print the variables glab reaches a checkout's GitLab with, one NAME=value a line.

The stand-in komrad puts ahead of an agent's PATH as glab runs this for every call.
--checkout names the tree it was run in, whose origin says which host to answer
for. --if-configured prints nothing and succeeds when the origin is not on a
connected GitLab host, which leaves glab the credential the machine was logged in
with.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			host := originHost(cmd.Context(), checkout)
			if host == "" {
				if ifConfigured {
					return nil
				}

				return errNoGitLabConnection.Saying("%s has no origin komrad can read a host from", checkout)
			}

			connection, token, err := gitlabToken(cmd.Context(), state, host)
			if isNoGitLabToken(err) && ifConfigured {
				return nil
			}
			if err != nil {
				return err
			}

			fmt.Fprint(cmd.OutOrStdout(), glabEnv(connection, token)) //nolint:errcheck // a closed stdout is the caller going away

			return nil
		},
	}

	cmd.Flags().StringVar(&checkout, "checkout", ".", "the checkout whose origin names the gitlab host")
	cmd.Flags().BoolVar(&ifConfigured, "if-configured", false, "print nothing and succeed when the host has no gitlab connection")

	return cmd
}

func gitlabCredentialCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "credential",
		Short: "Answer git's credential helper protocol with a GitLab connection's token",
		Long: `Answer git when it asks for a password.

Git runs this and describes what it wants to reach. This answers only for https to
a host connected as a GitLab forge with a token. Anything else gets no answer, so
git falls back to whatever else the account has configured.

Set it up for each GitLab host with:

    git config --global credential.https://gitlab.com.helper '!komrad gitlab credential'`,
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 || args[0] != "get" {
				return nil
			}

			input, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), 1<<16))
			if err != nil {
				return fmt.Errorf("failed to read what git asked about: %w", err)
			}

			query := githubapp.CredentialQuery(string(input))
			if !strings.EqualFold(query["protocol"], "https") || query["host"] == "" {
				return nil
			}

			_, token, err := gitlabToken(cmd.Context(), state, query["host"])
			if isNoGitLabToken(err) {
				return nil
			}
			if err != nil {
				return err
			}

			fmt.Fprint(cmd.OutOrStdout(), gitlabCredentialReply(token)) //nolint:errcheck // git closing the pipe is not this command failing

			return nil
		},
	}
}

// originHost is the host a checkout's origin is on, empty when it has none komrad can read.
func originHost(ctx context.Context, dir string) string {
	raw, err := git.Run(ctx, dir, "config", "--get", "remote.origin.url")
	if err != nil {
		return ""
	}

	remote, err := git.ParseRemoteURL(raw)
	if err != nil {
		return ""
	}

	return remote.Host
}

// gitlabToken is the GitLab connection a host has and its token, or errNoGitLabConnection.
func gitlabToken(ctx context.Context, state *app, host string) (*forgev1.Forge, forge.Secret, error) {
	var connection *forgev1.Forge
	err := state.withAPI(ctx, func(ctx context.Context, base string) error {
		resp, err := forgeClient(base).ListForges(ctx, connect.NewRequest(&emptypb.Empty{}))
		if err != nil {
			return err
		}

		connection = gitlabConnection(resp.Msg.GetForges(), host)

		return nil
	})
	if err != nil {
		return nil, forge.Secret{}, err
	}

	if connection == nil {
		return nil, forge.Secret{}, errNoGitLabConnection.Saying("%s is not connected as a gitlab forge with a token", host)
	}

	token, err := forge.NewFileStore(state.env.StateDir).Get(ctx, connection.GetId())
	if err != nil {
		return nil, forge.Secret{}, err
	}

	return connection, token, nil
}

// gitlabConnection is the GitLab connection holding a token for a host, nil when there is none.
func gitlabConnection(forges []*forgev1.Forge, host string) *forgev1.Forge {
	for _, f := range forges {
		if f.GetKind() == forge.KindGitLab && f.GetMethod() == forge.MethodToken &&
			f.GetHasCredential() && strings.EqualFold(f.GetHost(), host) {
			return f
		}
	}

	return nil
}

// isNoGitLabToken says whether a lookup failed only because the host has no GitLab token.
func isNoGitLabToken(err error) bool {
	return errors.Is(err, errNoGitLabConnection) || errors.Is(err, forge.ErrNoCredential)
}

// gitlabCredentialReply is git's credential answer for a GitLab token, which GitLab takes under
// any username.
func gitlabCredentialReply(token forge.Secret) string {
	return "username=oauth2\npassword=" + tokenForTheWire(token) + "\n"
}

// glabEnv is the environment glab reaches a connection with, one NAME=value a line.
func glabEnv(connection *forgev1.Forge, token forge.Secret) string {
	env := "GITLAB_TOKEN=" + tokenForTheWire(token) + "\n"
	if host := gitlabAPIHost(connection); host != "" {
		env += "GITLAB_API_HOST=" + host + "\n"
	}

	return env
}

// gitlabAPIHost is the host a connection's API answers on, empty when it is the connection's own.
func gitlabAPIHost(connection *forgev1.Forge) string {
	parsed, err := url.Parse(connection.GetApiUrl())
	if err != nil || parsed.Host == "" || strings.EqualFold(parsed.Host, connection.GetHost()) {
		return ""
	}

	return parsed.Host
}
