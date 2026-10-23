package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"komrad/internal/forge"
	"komrad/lib/dates"
	"komrad/lib/protoenum"
	"komrad/lib/xhttp"
	"komrad/lib/xslice"
	forgev1 "komrad/proto/gen/forge/v1"
	"komrad/proto/gen/forge/v1/forgev1connect"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/emptypb"
)

func forgeCmd(state *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "forge",
		Short: "Connect komrad to the services pull requests live on",
		Long: `Connect komrad to the services pull requests live on.

Komrad reads the pull requests its agents open through a connection to each forge,
made once and kept until it is disconnected. The server holds the connection: a
token lives in a file under its state directory that only its user can read, and
the row in its database records who the forge said the credential belongs to.
Every command here talks to the server, so with --engine it connects the engine.`,
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

    gh auth token | komrad forge connect github.com
    op read op://vault/github/token | komrad forge connect github.com
    komrad forge connect gitlab.com --token-file ~/.config/gitlab-token
    komrad forge connect gitlab.example.com --kind gitlab --token-file ~/.config/gitlab-token

A GitLab token is a personal access token with the api scope. A remote on an SSH
host of its own, such as ssh.gitlab.example.com, reaches the connection for its web
host once [git] ssh_hosts in the config names the pair.

With --method github-app nothing is read. The server mints a token from the App
named under [github_app] in its own config, checks the installation reaches at
least one repository, and records the App's own name as the login. That is the
engine's path, where nobody is there to paste a token and the App's key is already
on the machine: komrad forge connect github.com --method github-app --engine remote.

With --method rotating-token the token is read as for token, and the host has to
run GitLab. The token needs the api scope. The server rotates it at once and keeps
only the new one, then rotates it again each day before it expires, so the token
you pasted stops working as soon as the connection is made.

The server asks the forge who the credential belongs to before storing anything,
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
				ApiUrl: apiURL,
			}
			if kind != "" {
				found, err := forgeKindNamed(kind)
				if err != nil {
					return err
				}
				req.Kind = found
			}
			req.Method = methodNamed(method)
			if method != "" && req.GetMethod() == forgev1.CredentialMethod_CREDENTIAL_METHOD_UNSPECIFIED {
				return fmt.Errorf("--method is token, rotating-token or github-app, not %q", method)
			}
			if req.GetMethod() != forgev1.CredentialMethod_CREDENTIAL_METHOD_GITHUB_APP {
				token, err := readToken(cmd.InOrStdin(), tokenFile)
				if err != nil {
					return err
				}
				req.Token = tokenForTheWire(token)
			}

			return state.withAPI(cmd.Context(), func(ctx context.Context, base string) error {
				resp, err := state.forgeClient(base).ConnectForge(ctx, connect.NewRequest(req))
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
	cmd.Flags().StringVar(&method, "method", "token", "how the credential comes: token or rotating-token from stdin, or github-app from the App in the server's config")

	return cmd
}

func forgeListCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:           "list",
		Short:         "Show every connected forge and who komrad acts as there",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return state.withAPI(cmd.Context(), func(ctx context.Context, base string) error {
				resp, err := state.forgeClient(base).ListForges(ctx, connect.NewRequest(&emptypb.Empty{}))
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
				resp, err := state.forgeClient(base).CheckForges(ctx, connect.NewRequest(&emptypb.Empty{}))
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
				if _, err := state.forgeClient(base).DisconnectForge(ctx, connect.NewRequest(&forgev1.DisconnectForgeRequest{Host: args[0]})); err != nil {
					return err
				}

				cmd.Printf("disconnected %s\n", strings.ToLower(args[0]))

				return nil
			})
		},
	}
}

func (a *app) forgeClient(base string) forgev1connect.ForgeServiceClient {
	return forgev1connect.NewForgeServiceClient(xhttp.NewClientOver(a.signTransport(xhttp.NewTransport()), apiCallTimeout), base)
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
			verified = dates.Read(f.GetVerifiedAt()).Local().Format("2006-01-02 15:04")
		}

		rows = append(rows, []string{f.GetHost(), strings.ToLower(forge.Name(f.GetKind())), f.GetLogin(), methodFlags[f.GetMethod()], verified})
	}

	return printTable(out, []string{"HOST", "KIND", "LOGIN", "METHOD", "VERIFIED"}, rows)
}

// tokenForTheWire is the one place in the CLI a token leaves its Secret.
func tokenForTheWire(token forge.Secret) string {
	return token.Expose()
}

// readToken reads the token from a file when one is named, and otherwise from stdin.
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

// methodFlags is the --method value that names each way a credential is given.
var methodFlags = map[forgev1.CredentialMethod]string{
	forgev1.CredentialMethod_CREDENTIAL_METHOD_TOKEN:          "token",
	forgev1.CredentialMethod_CREDENTIAL_METHOD_ROTATING_TOKEN: "rotating-token",
	forgev1.CredentialMethod_CREDENTIAL_METHOD_GITHUB_APP:     "github-app",
}

// methodNamed is the way of giving a credential a --method value names, unspecified for none.
func methodNamed(name string) forgev1.CredentialMethod {
	for method, flag := range methodFlags {
		if flag == name {
			return method
		}
	}

	return forgev1.CredentialMethod_CREDENTIAL_METHOD_UNSPECIFIED
}

// forgeKindNamed is the kind of forge a --kind value names, such as github.
func forgeKindNamed(name string) (forgev1.Kind, error) {
	kinds := protoenum.Values[forgev1.Kind]()
	for _, kind := range kinds {
		if strings.EqualFold(forge.Name(kind), name) {
			return kind, nil
		}
	}

	names := xslice.Map(kinds, func(kind forgev1.Kind) string { return strings.ToLower(forge.Name(kind)) })

	return forgev1.Kind_KIND_UNSPECIFIED, fmt.Errorf("--kind is %s, not %q", strings.Join(names, " or "), name)
}
