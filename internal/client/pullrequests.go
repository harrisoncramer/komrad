package client

import (
	"context"

	forgev1 "weave/proto/gen/forge/v1"
	"weave/proto/gen/forge/v1/forgev1connect"
	paginationv1 "weave/proto/gen/pagination/v1"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
	"weave/proto/gen/pullrequest/v1/pullrequestv1connect"

	"connectrpc.com/connect"
)

// ListAllPullRequests reads every pull request the service has stored for a branch, following
// the page tokens until the listing is exhausted.
func ListAllPullRequests(ctx context.Context, pullRequests pullrequestv1connect.PullRequestServiceClient, repositoryURL, branch string) ([]*pullrequestv1.PullRequest, error) {
	return listAll(ctx, func(ctx context.Context, page *paginationv1.Pagination) ([]*pullrequestv1.PullRequest, string, error) {
		resp, err := pullRequests.ListPullRequests(ctx, connect.NewRequest(&pullrequestv1.ListPullRequestsRequest{
			RepositoryUrl: repositoryURL,
			HeadBranch:    branch,
			Pagination:    page,
		}))
		if err != nil {
			return nil, "", err
		}

		return resp.Msg.GetPullRequests(), resp.Msg.GetNextPageToken(), nil
	})
}

// ListAllForgePullRequests asks the forge for every pull request opened from the branch, or
// every open one when no branch is named, following the page tokens until the listing is
// exhausted.
func ListAllForgePullRequests(ctx context.Context, forges forgev1connect.ForgeServiceClient, remoteURL, branch string) ([]*forgev1.ForgePullRequest, error) {
	return listAll(ctx, func(ctx context.Context, page *paginationv1.Pagination) ([]*forgev1.ForgePullRequest, string, error) {
		resp, err := forges.ListForgePullRequests(ctx, connect.NewRequest(&forgev1.ListForgePullRequestsRequest{
			RemoteUrl:  remoteURL,
			Branch:     branch,
			Pagination: page,
		}))
		if err != nil {
			return nil, "", err
		}

		return resp.Msg.GetPullRequests(), resp.Msg.GetNextPageToken(), nil
	})
}
