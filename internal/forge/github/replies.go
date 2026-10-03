package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"komrad/internal/fail"
	"komrad/internal/forge"
	"komrad/lib/xhttp"
)

const addThreadReplyMutation = `mutation AddThreadReply($thread: ID!, $body: String!) {
  addPullRequestReviewThreadReply(input: {pullRequestReviewThreadId: $thread, body: $body}) { comment { id } }
}`

// ReplyToThread posts a reply in a pull request's thread and answers the new comment's id.
func (f *Forge) ReplyToThread(ctx context.Context, params forge.ReplyToThreadParams) (string, error) {
	owner, name, err := ownerAndName(params.Repo.Path)
	if err != nil {
		return "", fmt.Errorf("failed to reply on #%d: %w", params.Number, err)
	}
	if !params.OnCode {
		return f.commentOnConversation(ctx, commentOnConversationParams{
			Owner:  owner,
			Name:   name,
			Number: params.Number,
			Body:   params.Body,
		})
	}

	var data struct {
		AddPullRequestReviewThreadReply *struct {
			Comment *struct {
				ID string `json:"id"`
			} `json:"comment"`
		} `json:"addPullRequestReviewThreadReply"`
	}
	answer, err := f.client.Query(ctx, addThreadReplyMutation, map[string]any{
		"thread": params.ThreadID,
		"body":   params.Body,
	}, &data)
	if err != nil {
		return "", fmt.Errorf("failed to reply to thread %s: %w", params.ThreadID, err)
	}
	added := data.AddPullRequestReviewThreadReply
	if added != nil && added.Comment != nil && added.Comment.ID != "" {
		return added.Comment.ID, nil
	}
	if err := failureOf(answer); err != nil {
		if errors.Is(err, forge.ErrNotFound) {
			return "", fmt.Errorf("failed to reply to thread %s: %w", params.ThreadID, err)
		}

		return "", fmt.Errorf("%w: %w", forge.ErrReplyRefused, err)
	}

	return "", fmt.Errorf("github answered the reply to thread %s with no comment", params.ThreadID)
}

type commentOnConversationParams struct {
	Owner  string
	Name   string
	Number int
	Body   string
}

func (f *Forge) commentOnConversation(ctx context.Context, params commentOnConversationParams) (string, error) {
	var comment struct {
		NodeID string `json:"node_id"`
	}
	answer, err := f.client.Send(ctx, xhttp.JSONParams{
		Method: http.MethodPost,
		URL:    fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments", f.client.API(), params.Owner, params.Name, params.Number),
		Body:   map[string]string{"body": params.Body},
	}, &comment)
	if err != nil && fail.Of(err) == nil && answer.IsRefusal() {
		return "", fmt.Errorf("%w: %w", forge.ErrReplyRefused, err)
	}
	if err != nil {
		return "", fmt.Errorf("failed to comment on #%d: %w", params.Number, err)
	}
	if answer.Status == http.StatusNotFound {
		return "", noPullRequest(params.Number, params.Owner, params.Name)
	}
	if comment.NodeID == "" {
		return "", fmt.Errorf("github answered the comment on #%d with no node id", params.Number)
	}

	return comment.NodeID, nil
}
