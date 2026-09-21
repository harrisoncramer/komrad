package client

import (
	"context"

	paginationv1 "weave/proto/gen/pagination/v1"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
	"weave/proto/gen/pullrequest/v1/pullrequestv1connect"

	"connectrpc.com/connect"
)

// ListAllPullRequests returns every pull request opened from the branch, or every open one when
// no branch is named, following the page tokens until the listing is exhausted.
func ListAllPullRequests(ctx context.Context, pullRequests pullrequestv1connect.PullRequestServiceClient, remoteURL, branch string) ([]*pullrequestv1.PullRequest, error) {
	return listAll(ctx, func(ctx context.Context, page *paginationv1.Pagination) ([]*pullrequestv1.PullRequest, string, error) {
		resp, err := pullRequests.ListPullRequests(ctx, connect.NewRequest(&pullrequestv1.ListPullRequestsRequest{
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
