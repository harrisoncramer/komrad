package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"weave/config"
	"weave/internal/forge"
	"weave/internal/forge/github"
	"weave/internal/store"
	forgestore "weave/internal/store/forge"

	"github.com/spf13/cobra"
)

// forgeBuilders is every forge this build can connect to, by kind.
var forgeBuilders = map[string]forge.Builder{
	forge.KindGitHub: github.Build,
}

func forgeCmd(state *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "forge",
		Short: "Connect weave to the services pull requests live on",
		Long: `Connect weave to the services pull requests live on.

Weave reads the pull requests its agents open through a connection to each forge,
made once with a token and kept until it is disconnected. The token is held in a
file under weave's state directory that only your user can read, and the row in the
database records who the forge said the token belongs to.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.AddCommand(forgeConnectCmd(state), forgeListCmd(state), forgeDisconnectCmd(state))

	return cmd
}

func forgeConnectCmd(state *app) *cobra.Command {
	var kind, apiURL, tokenFile string

	cmd := &cobra.Command{
		Use:   "connect <host>",
		Short: "Connect a forge with a token read from stdin",
		Long: `Connect a forge with a token read from stdin.

The token is read from standard input, or from the file --token-file names. It is
never taken as an argument, since arguments are visible to every process on the
machine. Pipe it in:

    gh auth token | weave forge connect github.com
    op read op://vault/github/token | weave forge connect github.com

Weave asks the forge who the token belongs to before storing anything, so a token
that is wrong, expired or scoped to nothing is refused here rather than found out
later. github.com and gitlab.com say which forge they run; any other host needs
--kind, and --api-url when the API is not at the host's usual place.`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := readToken(cmd.InOrStdin(), tokenFile)
			if err != nil {
				return err
			}

			return withForgeService(cmd.Context(), state, func(ctx context.Context, service *forge.Service) error {
				connection, err := service.Connect(ctx, forge.ConnectParams{
					Host:   args[0],
					Kind:   kind,
					APIURL: apiURL,
					Token:  token,
				})
				if err != nil {
					return err
				}

				cmd.Printf("connected %s as %s\n", connection.Host, connection.Login)

				return nil
			})
		},
	}

	cmd.Flags().StringVar(&kind, "kind", "", "which forge the host runs, github or gitlab, when the host does not say")
	cmd.Flags().StringVar(&apiURL, "api-url", "", "where the forge answers API calls, for an enterprise host")
	cmd.Flags().StringVar(&tokenFile, "token-file", "", "read the token from this file instead of stdin")

	return cmd
}

func forgeListCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:           "list",
		Short:         "Show every connected forge and who weave acts as there",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withForgeService(cmd.Context(), state, func(ctx context.Context, service *forge.Service) error {
				connections, err := service.List(ctx)
				if err != nil {
					return err
				}
				if len(connections) == 0 {
					cmd.Println("no forges are connected")

					return nil
				}

				w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
				if _, err := fmt.Fprintln(w, "HOST\tKIND\tLOGIN\tMETHOD\tVERIFIED"); err != nil {
					return err
				}
				for _, connection := range connections {
					verified := "refused"
					if connection.VerifiedAt != nil {
						verified = connection.VerifiedAt.Local().Format("2006-01-02 15:04")
					}
					if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", connection.Host, connection.Kind, connection.Login, connection.Method, verified); err != nil {
						return err
					}
				}

				return w.Flush()
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
			return withForgeService(cmd.Context(), state, func(ctx context.Context, service *forge.Service) error {
				if err := service.Disconnect(ctx, args[0]); err != nil {
					return err
				}

				cmd.Printf("disconnected %s\n", strings.ToLower(args[0]))

				return nil
			})
		},
	}
}

// readToken is the token from a file when one is named, and otherwise from stdin.
func readToken(stdin io.Reader, tokenFile string) (string, error) {
	source := stdin
	if tokenFile != "" {
		file, err := os.Open(tokenFile)
		if err != nil {
			return "", fmt.Errorf("failed to open the token file: %w", err)
		}
		defer file.Close()
		source = file
	}

	raw, err := io.ReadAll(io.LimitReader(source, 4096))
	if err != nil {
		return "", fmt.Errorf("failed to read the token: %w", err)
	}

	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", fmt.Errorf("no token was given; pipe one in or name a file with --token-file")
	}

	return token, nil
}

func withForgeService(ctx context.Context, state *app, run func(context.Context, *forge.Service) error) error {
	pool, err := store.Open(ctx, state.env.DatabaseURL)
	if err != nil {
		return err
	}
	defer func() { _ = pool.Close() }()

	stateDir, err := config.GetStateDir()
	if err != nil {
		return err
	}

	service := forge.NewService(forge.NewServiceParams{
		Connections: forgestore.New(pool.GetDB()),
		StateDir:    stateDir,
		Builders:    forgeBuilders,
	})

	return run(ctx, service)
}
