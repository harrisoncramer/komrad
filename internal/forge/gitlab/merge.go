package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"komrad/internal/forge"
	"komrad/internal/forge/graphql"
	"komrad/lib/xhttp"
)

// Merge accepts the merge request, squashed or not as asked, or the way the project is set to
// merge when nothing was.
func (f *Forge) Merge(ctx context.Context, params forge.MergeParams) error {
	path, number, err := parseMergeRequestURL(params.URL)
	if err != nil {
		return err
	}
	body, err := f.mergeBody(ctx, path, number, params)
	if err != nil {
		return err
	}

	endpoint := fmt.Sprintf("%s/projects/%s/merge_requests/%d", f.client.API(), url.PathEscape(path), number)
	answer, err := f.client.Send(ctx, xhttp.JSONParams{
		Method: http.MethodPut,
		URL:    endpoint + "/merge",
		Body:   body,
	}, nil)
	if errors.Is(err, forge.ErrThrottled) {
		return err
	}

	switch answer.Status {
	case http.StatusNotFound:
		return noMergeRequest(number, path)
	case http.StatusForbidden:
		return forge.ErrUnauthorized.Saying("gitlab will not let the credential merge %s: %v", params.URL, err)
	case http.StatusMethodNotAllowed, http.StatusNotAcceptable, http.StatusConflict, http.StatusUnprocessableEntity:
		return forge.ErrNotMergeable.Saying("gitlab will not merge !%d: %s", number, f.mergeRefusal(ctx, endpoint, graphql.MessageOf(answer.Body)))
	}

	return err
}

// mergeRefusal is why GitLab says a merge request cannot merge, in a person's words.
func (f *Forge) mergeRefusal(ctx context.Context, endpoint string, said string) string {
	var mergeRequest struct {
		State               string `json:"state"`
		Draft               bool   `json:"draft"`
		DetailedMergeStatus string `json:"detailed_merge_status"`
	}
	if _, err := f.client.Send(ctx, xhttp.JSONParams{Method: http.MethodGet, URL: endpoint}, &mergeRequest); err != nil {
		return said
	}

	switch mergeRequest.State {
	case "merged":
		return "it has already merged"
	case "closed":
		return "it was closed without merging"
	case "locked":
		return "it is already being merged"
	}
	if mergeRequest.Draft {
		return "it is still a draft"
	}

	switch mergeRequest.DetailedMergeStatus {
	case "unchecked", "checking", "approvals_syncing", "preparing":
		return "gitlab is still checking whether it can merge, so try again in a moment"
	case "conflict":
		return "it has merge conflicts"
	case "draft_status":
		return "it is still a draft"
	case "need_rebase":
		return "it has to be rebased onto its target first"
	case "ci_must_pass", "ci_still_running":
		return "its pipeline has to pass first"
	case "not_approved", "requested_changes":
		return "it has not been approved"
	case "discussions_not_resolved":
		return "its threads are not all resolved"
	case "", "mergeable":
		return said
	}

	return "its merge status is " + strings.ReplaceAll(mergeRequest.DetailedMergeStatus, "_", " ")
}

// mergeability is the detailed merge status in komrad's words, unknown once the merge request
// has merged or closed.
func (n mergeRequestNode) mergeability() forge.Mergeability {
	if n.State == "merged" || n.State == "closed" {
		return forge.MergeabilityUnknown
	}

	switch n.DetailedMergeStatus {
	case "":
		return forge.MergeabilityUnknown
	case "MERGEABLE":
		return forge.MergeabilityClean
	case "CI_MUST_PASS", "CI_STILL_RUNNING":
		return forge.MergeabilityUnstable
	case "NEED_REBASE":
		return forge.MergeabilityBehind
	case "CONFLICT":
		return forge.MergeabilityConflicting
	case "UNCHECKED", "CHECKING", "PREPARING", "APPROVALS_SYNCING":
		return forge.MergeabilityUnknown
	}

	return forge.MergeabilityBlocked
}

// joinMessage is a commit message as one text, its headline apart from its body by a blank line.
func joinMessage(message forge.CommitMessage) string {
	if message.Body == "" {
		return message.Headline
	}

	return message.Headline + "\n\n" + message.Body
}

// mergeBody is the REST fields that say how a merge request merges, empty to leave it to the
// project.
func (f *Forge) mergeBody(ctx context.Context, path string, number int, params forge.MergeParams) (map[string]any, error) {
	body := map[string]any{}
	switch params.Method {
	case "":
	case forge.MergeMethodSquash:
		body["squash"] = true
	case forge.MergeMethodMerge, forge.MergeMethodFastForward:
		body["squash"] = false
	case forge.MergeMethodRebase:
		return nil, forge.ErrNoMergeMethod.Saying("gitlab cannot merge by %s", params.Method)
	}

	message, err := f.commitMessage(ctx, path, number, params)
	if err != nil {
		return nil, err
	}
	if message != "" {
		field := "merge_commit_message"
		if params.Method == forge.MergeMethodSquash {
			field = "squash_commit_message"
		}
		body[field] = message
	}
	if params.HeadSHA != "" {
		body["sha"] = params.HeadSHA
	}

	return body, nil
}

// commitMessage is the merge's commit message as one text, any part left unset read from the
// one GitLab would write, and empty when both are unset.
func (f *Forge) commitMessage(ctx context.Context, path string, number int, params forge.MergeParams) (string, error) {
	if params.Headline == nil && params.Body == nil {
		return "", nil
	}

	var message forge.CommitMessage
	if params.Headline == nil || params.Body == nil {
		target, err := f.readPlanTarget(ctx, path, number)
		if err != nil {
			return "", err
		}

		written := target.DefaultMergeCommitMessage
		if params.Method == forge.MergeMethodSquash {
			written = target.DefaultSquashCommitMessage
		}
		if split := splitMessage(written); split != nil {
			message = *split
		}
	}
	if params.Headline != nil {
		message.Headline = *params.Headline
	}
	if params.Body != nil {
		message.Body = *params.Body
	}

	return joinMessage(message), nil
}
