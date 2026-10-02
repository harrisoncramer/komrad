package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"

	"komrad/internal/fail"
	"komrad/internal/forge"
	githubapp "komrad/internal/github/app"
	forgev1 "komrad/proto/gen/forge/v1"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/emptypb"
)

// errNoGitLabConnection says komrad holds no GitLab token for a host.
var errNoGitLabConnection = fail.Precondition("no gitlab forge with a token is connected for this host")

// errTokenSpansLines refuses a token that would break out of the line it is handed over on.
var errTokenSpansLines = fail.Precondition("the gitlab connection's token holds a line break, so komrad will not hand it out")

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

	cmd.AddCommand(gitlabConfigCmd(state), gitlabCredentialCmd(state))

	return cmd
}

func gitlabConfigCmd(state *app) *cobra.Command {
	var (
		checkout     string
		ifConfigured bool
	)

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Print the glab config a checkout's GitLab is reached with",
		Long: `Print the glab config a checkout's GitLab is reached with, as glab's config.yml.

The stand-in komrad puts ahead of an agent's PATH as glab runs this for every call.
--checkout names the tree it was run in, whose origin says which host to answer
for. The config names that host alone, so glab hands the token to no other host.
--if-configured prints nothing and succeeds when the origin is not on a
connected GitLab host, which leaves glab the credential the machine was logged in
with.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			host := state.gitClient().ReadOriginHost(cmd.Context(), checkout)
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

			fmt.Fprint(cmd.OutOrStdout(), glabConfig(connection, token)) //nolint:errcheck // a closed stdout is the caller going away

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

			_, token, err := gitlabToken(cmd.Context(), state, withoutPort(query["host"]))
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

// withoutPort is a host git described, less any port it was reached on.
func withoutPort(host string) string {
	name, _, err := net.SplitHostPort(host)
	if err != nil {
		return host
	}

	return name
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

	if err := requireSingleLineToken(token); err != nil {
		return nil, forge.Secret{}, err
	}

	return connection, token, nil
}

// gitlabConnection is the GitLab connection holding a token for a host, nil when there is none.
func gitlabConnection(forges []*forgev1.Forge, host string) *forgev1.Forge {
	for _, f := range forges {
		if f.GetKind() == forge.KindGitLab && f.GetHasCredential() && strings.EqualFold(f.GetHost(), host) {
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

// glabConfig is a glab config.yml that knows only the connection's host and its token.
func glabConfig(connection *forgev1.Forge, token forge.Secret) string {
	host := strconv.Quote(connection.GetHost())

	var config strings.Builder
	fmt.Fprintf(&config, "host: %s\nhosts:\n  %s:\n    token: %s\n", host, host, strconv.Quote(tokenForTheWire(token)))

	api, err := url.Parse(connection.GetApiUrl())
	if err != nil || api.Host == "" {
		return config.String()
	}
	if !strings.EqualFold(api.Host, connection.GetHost()) {
		fmt.Fprintf(&config, "    api_host: %s\n", strconv.Quote(api.Host))
	}
	if strings.EqualFold(api.Scheme, "http") {
		config.WriteString("    api_protocol: http\n")
	}

	return config.String()
}

// requireSingleLineToken refuses a token holding a line break, which would spill into the lines after it.
func requireSingleLineToken(token forge.Secret) error {
	if strings.ContainsAny(tokenForTheWire(token), "\r\n") {
		return errTokenSpansLines
	}

	return nil
}
