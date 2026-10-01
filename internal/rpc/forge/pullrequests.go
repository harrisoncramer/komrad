package forge

import (
	"context"
	"fmt"

	"komrad/internal/fail"
	"komrad/internal/forge"
	"komrad/internal/git"
	"komrad/internal/rpcerr"
	"komrad/lib/dates"
	"komrad/lib/pagination"
	"komrad/lib/xslice"
	forgev1 "komrad/proto/gen/forge/v1"

	"google.golang.org/protobuf/types/known/emptypb"
)

// pageCursor is where a listing left off, the forge's cursor for the next page and the size
// the pages before it were cut at.
type pageCursor struct {
	Cursor  string `json:"cursor"`
	PerPage int    `json:"per_page"`
}

// Validate refuses a page nobody could have been handed.
func (c pageCursor) Validate() error {
	if c.Cursor == "" {
		return fmt.Errorf("a page token names no cursor")
	}
	if c.PerPage < 1 {
		return fmt.Errorf("%d to a page is no page", c.PerPage)
	}

	return nil
}

// ListForgePullRequests is one page of a repository's pull requests, read through the
// connection for the repository's host.
func (s *Handler) ListForgePullRequests(ctx context.Context, req *forgev1.ListForgePullRequestsRequest) (*forgev1.ListForgePullRequestsResponse, *fail.Error) {
	repo, err := git.ParseRemoteURL(req.GetRemoteUrl())
	if err != nil {
		return nil, rpcerr.From(err, "failed to read the remote")
	}

	window := pageCursor{PerPage: int(pagination.Bound(req.GetPagination().GetLimit()))}
	if cursor, err := pagination.Decode[pageCursor](req.GetPagination().GetCursor()); err != nil {
		return nil, rpcerr.From(err, "failed to read the page token")
	} else if cursor != nil {
		window = *cursor
	}

	connection, err := s.forges.ConnectionFor(ctx, repo.Host)
	if err != nil {
		return nil, rpcerr.From(err, "failed to find the forge for the remote")
	}

	f, err := s.forges.ForConnection(*connection)
	if err != nil {
		return nil, rpcerr.From(err, "failed to build the forge")
	}

	listed, err := f.List(ctx, forge.ListParams{
		Repo:    repo,
		Branch:  req.GetBranch(),
		Cursor:  window.Cursor,
		PerPage: window.PerPage,
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the pull requests")
	}

	response := &forgev1.ListForgePullRequestsResponse{PullRequests: xslice.Map(listed.PullRequests, renderForgePullRequest)}
	if listed.HasMore {
		next, err := pagination.Token(pageCursor{Cursor: listed.Cursor, PerPage: window.PerPage})
		if err != nil {
			return nil, rpcerr.From(err, "failed to build the next page token")
		}

		response.NextPageToken = next
	}

	return response, nil
}

// MergeForgePullRequest merges a pull request through the connection for its host.
func (s *Handler) MergeForgePullRequest(ctx context.Context, req *forgev1.MergeForgePullRequestRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.writer.MergePullRequest(ctx, req.GetUrl()); err != nil {
		return nil, rpcerr.From(err, "failed to merge the pull request")
	}

	return &emptypb.Empty{}, nil
}

// SetForgePullRequestDraft marks a pull request a draft or ready through the connection for its
// host.
func (s *Handler) SetForgePullRequestDraft(ctx context.Context, req *forgev1.SetForgePullRequestDraftRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.writer.SetPullRequestDraft(ctx, req.GetUrl(), req.GetDraft()); err != nil {
		return nil, rpcerr.From(err, "failed to change whether the pull request is a draft")
	}

	return &emptypb.Empty{}, nil
}

// SetForgePullRequestAutoMerge asks the forge to merge a pull request once its checks and reviews
// pass, or stops it, through the connection for its host.
func (s *Handler) SetForgePullRequestAutoMerge(ctx context.Context, req *forgev1.SetForgePullRequestAutoMergeRequest) (*emptypb.Empty, *fail.Error) {
	err := s.writer.SetPullRequestAutoMerge(ctx, forge.SetAutoMergeParams{
		URL:     req.GetUrl(),
		HeadSHA: req.GetHeadSha(),
		On:      req.GetOn(),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to change auto-merge on the pull request")
	}

	return &emptypb.Empty{}, nil
}

func renderForgePullRequest(pullRequest forge.PullRequest) *forgev1.ForgePullRequest {
	head := ""
	if !pullRequest.Head.IsZero() {
		head = pullRequest.Head.Canonical()
	}

	return &forgev1.ForgePullRequest{
		Forge:             pullRequest.Forge,
		ProviderId:        pullRequest.ProviderID,
		Url:               pullRequest.URL,
		RepositoryUrl:     pullRequest.Repository.Canonical(),
		HeadRepositoryUrl: head,
		Number:            int32(pullRequest.Number),
		Title:             pullRequest.Title,
		HeadBranch:        pullRequest.HeadBranch,
		HeadSha:           pullRequest.HeadSHA,
		BaseBranch:        pullRequest.BaseBranch,
		BaseSha:           pullRequest.BaseSHA,
		Draft:             pullRequest.Draft,
		State:             pullRequest.State,
		CreatedAt:         dates.Stamp(pullRequest.CreatedAt),
		UpdatedAt:         dates.Stamp(pullRequest.UpdatedAt),
		MergedAt:          dates.StampPtr(pullRequest.MergedAt),
		ClosedAt:          dates.StampPtr(pullRequest.ClosedAt),
	}
}
