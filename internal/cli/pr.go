package cli

import (
	"context"
	"fmt"
	"text/tabwriter"

	"weave/lib/xhttp"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
	"weave/proto/gen/pullrequest/v1/pullrequestv1connect"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
)

func prCmd(state *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "pr",
		Short:         "Read pull requests through the forges weave is connected to",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.AddCommand(prListCmd(state))

	return cmd
}

func prListCmd(state *app) *cobra.Command {
	return &cobra.Command{
		Use:   "list <remote-url> [branch]",
		Short: "List a repository's pull requests, or those opened from one branch",
		Long: `List a repository's pull requests through the forge its host is connected to.

Give a remote as git spells it. With a branch, every pull request opened from it is
listed whatever its state, which is the read the pull request sweep makes; without
one, the repository's open pull requests are listed.

    weave pr list git@github.com:harrisoncramer/weave.git
    weave pr list git@github.com:harrisoncramer/weave.git agent/some-branch

A repository the credential cannot see answers not found, which is how a token
without access to a private repository shows up.`,
		Args:          cobra.RangeArgs(1, 2),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return state.withAPI(cmd.Context(), func(ctx context.Context, base string) error {
				client := pullrequestv1connect.NewPullRequestServiceClient(xhttp.NewClient(apiCallTimeout), base)
				branch := ""
				if len(args) == 2 {
					branch = args[1]
				}

				resp, err := client.ListPullRequests(ctx, connect.NewRequest(&pullrequestv1.ListPullRequestsRequest{RemoteUrl: args[0], Branch: branch}))
				if err != nil {
					return err
				}

				pullRequests := resp.Msg.GetPullRequests()
				if len(pullRequests) == 0 {
					cmd.Println("no pull requests")

					return nil
				}

				w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
				if _, err := fmt.Fprintln(w, "NUMBER\tSTATE\tTITLE\tURL"); err != nil {
					return err
				}
				for _, pullRequest := range pullRequests {
					if _, err := fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", pullRequest.GetNumber(), stateOf(pullRequest), pullRequest.GetTitle(), pullRequest.GetUrl()); err != nil {
						return err
					}
				}

				return w.Flush()
			})
		},
	}
}

// stateOf is the one word for where a pull request stands, derived from its stamps.
func stateOf(pullRequest *pullrequestv1.PullRequest) string {
	switch {
	case pullRequest.MergedAt != nil:
		return "merged"
	case pullRequest.ClosedAt != nil:
		return "closed"
	case pullRequest.GetDraft():
		return "draft"
	default:
		return "open"
	}
}
