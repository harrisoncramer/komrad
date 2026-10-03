package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"komrad/internal/fail"
	"komrad/internal/forge"
	"komrad/lib/xhttp"
)

const discussionPrefix = "gid://gitlab/Discussion/"

// ReplyToThread posts a reply in a pull request's thread and answers the new comment's id.
func (f *Forge) ReplyToThread(ctx context.Context, params forge.ReplyToThreadParams) (string, error) {
	path, err := projectPath(params.Repo.Path)
	if err != nil {
		return "", err
	}
	discussion, ok := strings.CutPrefix(params.ThreadID, discussionPrefix)
	if !ok || discussion == "" {
		return "", fmt.Errorf("%w: %q is not a gitlab discussion", forge.ErrNotFound, params.ThreadID)
	}

	var note struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	}
	answer, err := f.client.Send(ctx, xhttp.JSONParams{
		Method: http.MethodPost,
		URL:    fmt.Sprintf("%s/projects/%s/merge_requests/%d/discussions/%s/notes", f.client.API(), url.PathEscape(path), params.Number, url.PathEscape(discussion)),
		Body:   map[string]string{"body": params.Body},
	}, &note)
	if errors.Is(err, forge.ErrThrottled) {
		return "", err
	}

	switch answer.Status {
	case http.StatusNotFound:
		return "", noMergeRequest(params.Number, path)
	case http.StatusForbidden:
		return "", forge.ErrUnauthorized.Saying("gitlab will not let the credential reply on !%d: %v", params.Number, err)
	}
	if err != nil && fail.Of(err) == nil && answer.IsRefusal() {
		return "", fmt.Errorf("%w: %w", forge.ErrReplyRefused, err)
	}
	if err != nil {
		return "", err
	}
	if note.ID == 0 {
		return "", fmt.Errorf("gitlab answered the reply on !%d with no note", params.Number)
	}

	model := note.Type
	if model == "" {
		model = "Note"
	}

	return fmt.Sprintf("gid://gitlab/%s/%d", model, note.ID), nil
}
