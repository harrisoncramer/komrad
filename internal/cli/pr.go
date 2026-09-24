package cli

import (
	"context"
	"strconv"

	"komrade/internal/client"
	"komrade/lib/xhttp"
	"komrade/lib/xslice"
	forgev1 "komrade/proto/gen/forge/v1"
	"komrade/proto/gen/forge/v1/forgev1connect"

	"github.com/spf13/cobra"
)

func prCmd(state *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "pr",
		Short:         "Read pull requests through the forges komrade is connected to",
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

    komrade pr list git@github.com:harrisoncramer/komrade.git
    komrade pr list git@github.com:harrisoncramer/komrade.git agent/some-branch

The forge hands them over a hundred at a time and this follows every page, so the
list is whole; to read one page at a time, call the endpoint through komrade rpc
with a limit and the cursor it answers.

A repository the credential cannot see answers not found, which is how a token
without access to a private repository shows up.`,
		Args:          cobra.RangeArgs(1, 2),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return state.withAPI(cmd.Context(), func(ctx context.Context, base string) error {
				branch := ""
				if len(args) == 2 {
					branch = args[1]
				}

				pullRequests, err := client.ListAllForgePullRequests(ctx, forgev1connect.NewForgeServiceClient(xhttp.NewClient(apiCallTimeout), base), args[0], branch)
				if err != nil {
					return err
				}

				if len(pullRequests) == 0 {
					cmd.Println("no pull requests")

					return nil
				}

				return printTable(cmd.OutOrStdout(), []string{"NUMBER", "STATE", "TITLE", "URL"}, xslice.Map(pullRequests, pullRequestRow))
			})
		},
	}
}

func pullRequestRow(pullRequest *forgev1.ForgePullRequest) []string {
	return []string{strconv.FormatInt(int64(pullRequest.GetNumber()), 10), stateOf(pullRequest), pullRequest.GetTitle(), pullRequest.GetUrl()}
}

// stateOf is the one word for where a pull request stands.
func stateOf(pullRequest *forgev1.ForgePullRequest) string {
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
