package client

import (
	"context"

	forgev1 "komrade/proto/gen/forge/v1"
	"komrade/proto/gen/forge/v1/forgev1connect"
	paginationv1 "komrade/proto/gen/pagination/v1"

	"connectrpc.com/connect"
)

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
