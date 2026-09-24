package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"komrade/internal/forge"
	"komrade/lib/dates"
	"komrade/lib/xhttp"
	forgev1 "komrade/proto/gen/forge/v1"
	"komrade/proto/gen/forge/v1/forgev1connect"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/emptypb"
)

func forgeCmd(state *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "forge",
		Short: "Connect komrade to the services pull requests live on",
		Long: `Connect komrade to the services pull requests live on.

Komrade reads the pull requests its agents open through a connection to each forge,
made once and kept until it is disconnected. The service holds the connection: a
token lives in a file under its state directory that only its user can read, and
the row in its database records who the forge said the credential belongs to.
Every command here talks to the service, so with --engine it connects the engine.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.AddCommand(forgeConnectCmd(state), forgeListCmd(state), forgeCheckCmd(state), forgeDisconnectCmd(state))

	return cmd
}

func forgeConnectCmd(state *app) *cobra.Command {
	var kind, apiURL, tokenFile, method string

	cmd := &cobra.Command{
		Use:   "connect <host>",
		Short: "Connect a forge with a token read from stdin, or as the engine's GitHub App",
		Long: `Connect a forge with a token read from stdin, or as the engine's GitHub App.

With --method token, the default, the token is read from standard input, or from
the file --token-file names. It is never taken as an argument, since arguments are
visible to every process on the machine. Pipe it in:

    gh auth token | komrade forge connect github.com
    op read op://vault/github/token | komrade forge connect github.com

With --method github-app nothing is read. The service mints a token from the App
named under [github_app] in its own config, checks the installation reaches at
least one repository, and records the App's own name as the login. That is the
engine's path, where nobody is there to paste a token and the App's key is already
on the machine: komrade forge connect github.com --method github-app --engine remote.

The service asks the forge who the credential belongs to before storing anything,
so a token that is wrong or expired is refused here rather than found out later.
That proves the token is real, not that it can read your repositories: a token with
no permissions answers the same question, and shows up later as a repository the
forge says it cannot find. github.com and gitlab.com say which forge they run; any
other host needs --kind, and --api-url when the API is not at https://<host>/api/v3
for GitHub or /api/v4 for GitLab. The address has to be https.`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			req := &forgev1.ConnectForgeRequest{
				Host:   args[0],
				Kind:   kind,
				ApiUrl: apiURL,
			}
			switch method {
			case "token", "":
				token, err := readToken(cmd.InOrStdin(), tokenFile)
				if err != nil {
					return err
				}

				req.Method, req.Token = forge.MethodToken, tokenForTheWire(token)
			case "github-app":
				req.Method = forge.MethodGitHubApp
			default:
				return fmt.Errorf("--method is token or github-app, not %q", method)
			}

			return state.withAPI(cmd.Context(), func(ctx context.Context, base string) error {
				resp, err := forgeClient(base).ConnectForge(ctx, connect.NewRequest(req))
				if err != nil {
					return err
				}

				cmd.Printf("connected %s as %s\n", resp.Msg.GetForge().GetHost(), resp.Msg.GetForge().GetLogin())

				return nil
			})
		},
	}

	cmd.Flags().StringVar(&kind, "kind", "", "which forge the host runs, github or gitlab, when the host does not say")
	cmd.Flags().StringVar(&apiURL, "api-url", "", "where the forge answers API calls, for an enterprise host")
	cmd.Flags().StringVar(&tokenFile, "token-file", "", "read the token from this file instead of stdin")
	cmd.Flags().StringVar(&method, "method", "token", "how the credential comes: token from stdin, or github-app from the App in the service's config")

	return cmd
}

func forgeListCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:           "list",
		Short:         "Show every connected forge and who komrade acts as there",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return state.withAPI(cmd.Context(), func(ctx context.Context, base string) error {
				resp, err := forgeClient(base).ListForges(ctx, connect.NewRequest(&emptypb.Empty{}))
				if err != nil {
					return err
				}

				return printForges(cmd.OutOrStdout(), resp.Msg.GetForges(), nil)
			})
		},
	}
}

func forgeCheckCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Ask every connected forge whether its credential still works",
		Long: `Ask every connected forge whether its credential still works, the way its method
allows, and record the answer on each connection. A refusal marks the connection
until it is connected again.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return state.withAPI(cmd.Context(), func(ctx context.Context, base string) error {
				resp, err := forgeClient(base).CheckForges(ctx, connect.NewRequest(&emptypb.Empty{}))
				if err != nil {
					return err
				}

				forges := make([]*forgev1.Forge, 0, len(resp.Msg.GetChecks()))
				problems := make(map[string]string, len(resp.Msg.GetChecks()))
				for _, check := range resp.Msg.GetChecks() {
					forges = append(forges, check.GetForge())
					if check.GetProblem() != "" {
						problems[check.GetForge().GetHost()] = check.GetProblem()
					}
				}

				return printForges(cmd.OutOrStdout(), forges, problems)
			})
		},
	}
}

func forgeDisconnectCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:           "disconnect <host>",
		Short:         "Forget a forge and the token it was connected with",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return state.withAPI(cmd.Context(), func(ctx context.Context, base string) error {
				if _, err := forgeClient(base).DisconnectForge(ctx, connect.NewRequest(&forgev1.DisconnectForgeRequest{Host: args[0]})); err != nil {
					return err
				}

				cmd.Printf("disconnected %s\n", strings.ToLower(args[0]))

				return nil
			})
		},
	}
}

func forgeClient(base string) forgev1connect.ForgeServiceClient {
	return forgev1connect.NewForgeServiceClient(xhttp.NewClient(apiCallTimeout), base)
}

// printForges writes the table forge list and forge check share, with the problem a check
// found in place of the verified stamp.
func printForges(out io.Writer, forges []*forgev1.Forge, problems map[string]string) error {
	if len(forges) == 0 {
		_, err := fmt.Fprintln(out, "no forges are connected")

		return err
	}

	rows := make([][]string, 0, len(forges))
	for _, f := range forges {
		verified := "refused"
		switch {
		case problems[f.GetHost()] != "":
			verified = problems[f.GetHost()]
		case !f.GetHasCredential():
			verified = "no credential"
		case f.VerifiedAt != nil:
			verified = describeVerifiedAt(f.GetVerifiedAt())
		}

		rows = append(rows, []string{f.GetHost(), f.GetKind(), f.GetLogin(), f.GetMethod(), verified})
	}

	return printTable(out, []string{"HOST", "KIND", "LOGIN", "METHOD", "VERIFIED"}, rows)
}

// describeVerifiedAt is the stamp as a person reads it, in local time.
func describeVerifiedAt(stamp string) string {
	parsed := dates.Parse(stamp)
	if parsed.IsZero() {
		return stamp
	}

	return parsed.Local().Format("2006-01-02 15:04")
}

// tokenForTheWire is the one place in the CLI a token leaves its Secret.
func tokenForTheWire(token forge.Secret) string {
	return token.Expose()
}

// readToken is the token from a file when one is named, and otherwise from stdin.
func readToken(stdin io.Reader, tokenFile string) (forge.Secret, error) {
	source := stdin
	if tokenFile != "" {
		file, err := os.Open(tokenFile)
		if err != nil {
			return forge.Secret{}, fmt.Errorf("failed to open the token file: %w", err)
		}
		defer file.Close()
		source = file
	}

	raw, err := io.ReadAll(io.LimitReader(source, 4096))
	if err != nil {
		return forge.Secret{}, fmt.Errorf("failed to read the token: %w", err)
	}

	token := forge.NewSecret(string(raw))
	if token.IsZero() {
		return forge.Secret{}, fmt.Errorf("no token was given; pipe one in or name a file with --token-file")
	}

	return token, nil
}
