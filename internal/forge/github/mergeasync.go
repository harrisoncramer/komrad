package github

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"komrad/internal/forge"
	"komrad/internal/forge/graphql"
	"komrad/lib/clock"
	"komrad/lib/xhttp"
)

const (
	// mergePoll is how long a merge waits between reads of how GitHub's asynchronous merge stands.
	mergePoll = time.Second
	// mergePolls is how many reads a merge makes before it leaves the rest to the pull request sync.
	mergePolls = 10
)

// mergeAsyncAnswer is how GitHub says an asynchronous merge stands.
type mergeAsyncAnswer struct {
	Status  string `json:"status"`
	Details struct {
		Message string `json:"message"`
		UUID    string `json:"uuid"`
	} `json:"details"`
}

// refuseUnderOpen turns down a merge of a pull request with an open one under it in its GitHub stack,
// which GitHub would merge along with it.
func (f *Forge) refuseUnderOpen(ctx context.Context, owner, name string, number int) error {
	stacks, err := f.listStacks(ctx, owner, name, number)
	if err != nil {
		return err
	}

	for _, stack := range stacks {
		for _, under := range stack.PullRequests {
			if under.Number == number {
				break
			}
			if under.State == "open" {
				return forge.ErrNotMergeable.Saying("github will not merge #%d alone: #%d under it in its stack is still open", number, under.Number)
			}
		}
	}

	return nil
}

// mergeAsyncParams is the pull request to merge and the method GitHub names for it.
type mergeAsyncParams struct {
	Owner  string
	Name   string
	Number int
	Method string
	Merge  forge.MergeParams
}

// mergeAsync asks GitHub to merge a pull request and reads how the merge stands until it finishes or
// mergePolls runs out, leaving a merge still pending to the pull request sync.
func (f *Forge) mergeAsync(ctx context.Context, params mergeAsyncParams) error {
	address := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/merge-async", f.client.API(), params.Owner, params.Name, params.Number)
	body := map[string]string{"merge_method": params.Method}
	if params.Merge.HeadSHA != "" {
		body["sha"] = params.Merge.HeadSHA
	}
	if params.Merge.Headline != nil {
		body["commit_title"] = *params.Merge.Headline
	}
	if params.Merge.Body != nil {
		body["commit_message"] = *params.Merge.Body
	}

	var merge mergeAsyncAnswer
	answer, err := f.client.Send(ctx, xhttp.JSONParams{Method: http.MethodPut, URL: address, Body: body}, &merge)
	if answer.Status == http.StatusNotFound {
		return noPullRequest(params.Number, params.Owner, params.Name)
	}
	if isRefusal(answer, err) {
		return forge.ErrNotMergeable.Saying("github will not merge #%d: %s", params.Number, graphql.MessageOf(answer.Body))
	}
	if err != nil {
		return fmt.Errorf("failed to merge #%d: %w", params.Number, err)
	}

	uuid := merge.Details.UUID
	for range mergePolls {
		if merge.Status != "pending" {
			break
		}

		clock.Sleep(ctx, f.mergePoll)
		if err := ctx.Err(); err != nil {
			return err
		}

		if _, err := f.client.Send(ctx, xhttp.JSONParams{Method: http.MethodGet, URL: address + "/" + uuid}, &merge); err != nil {
			return fmt.Errorf("failed to read how the merge of #%d stands: %w", params.Number, err)
		}
	}

	if merge.Status == "failed" {
		return forge.ErrNotMergeable.Saying("github could not merge #%d: %s", params.Number, merge.Details.Message)
	}

	return nil
}
