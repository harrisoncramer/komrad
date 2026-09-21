package client

import (
	"context"

	paginationv1 "weave/proto/gen/pagination/v1"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
	"weave/proto/gen/pullrequest/v1/pullrequestv1connect"

	"connectrpc.com/connect"
)

// ListAllPullRequests returns every pull request the request names, following the page tokens
// until the listing is exhausted.
func ListAllPullRequests(ctx context.Context, pullRequests pullrequestv1connect.PullRequestServiceClient, req *pullrequestv1.ListPullRequestsRequest) ([]*pullrequestv1.PullRequest, error) {
	return listAll(ctx, func(ctx context.Context, page *paginationv1.Pagination) ([]*pullrequestv1.PullRequest, string, error) {
		req.Pagination = page

		resp, err := pullRequests.ListPullRequests(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, "", err
		}

		return resp.Msg.GetPullRequests(), resp.Msg.GetNextPageToken(), nil
	})
}
