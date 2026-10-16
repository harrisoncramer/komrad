package github

import (
	"context"
	_ "embed"

	"komrad/internal/forge"
	"komrad/lib/xptr"
)

//go:embed mergetarget.gql
var mergeTargetQuery string

//go:embed merge.gql
var mergeMutation string

// Merge lands the pull request by the method asked for, or by one the repository and its base
// branch allow when none was.
func (f *Forge) Merge(ctx context.Context, params forge.MergeParams) error {
	owner, name, number, err := parsePullURL(params.URL)
	if err != nil {
		return err
	}

	var data struct {
		Repository *struct {
			mergeSettings
			PullRequest *struct {
				ID      string   `json:"id"`
				State   string   `json:"state"`
				BaseRef *baseRef `json:"baseRef"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}
	answer, err := f.client.Query(ctx, mergeTargetQuery, map[string]any{
		"owner":  owner,
		"name":   name,
		"number": number,
	}, &data)
	if err != nil {
		return err
	}
	if err := failureOf(answer); err != nil {
		return err
	}
	if data.Repository == nil || data.Repository.PullRequest == nil {
		return noPullRequest(number, owner, name)
	}
	switch data.Repository.PullRequest.State {
	case "MERGED":
		return forge.ErrNotMergeable.Saying("github will not merge #%d: it has already merged", number)
	case "CLOSED":
		return forge.ErrNotMergeable.Saying("github will not merge #%d: it was closed without merging", number)
	}

	method, err := data.Repository.methodFor(data.Repository.PullRequest.BaseRef, params.Method)
	if err != nil {
		return err
	}

	variables := messageVariables(params.Message)
	variables["id"] = data.Repository.PullRequest.ID
	variables["method"] = method
	variables["head"] = xptr.NonEmpty(params.HeadSHA)

	var merged struct{}
	answer, err = f.client.Query(ctx, mergeMutation, variables, &merged)
	if err != nil {
		return err
	}

	return mutationRefusal{sentinel: forge.ErrNotMergeable, verb: "merge"}.explain(answer, params.URL, number)
}

// mergeabilityOf is GitHub's mergeStateStatus in komrad's words.
func mergeabilityOf(status string) forge.Mergeability {
	switch status {
	case "CLEAN", "HAS_HOOKS":
		return forge.MergeabilityClean
	case "UNSTABLE":
		return forge.MergeabilityUnstable
	case "BLOCKED", "DRAFT":
		return forge.MergeabilityBlocked
	case "BEHIND":
		return forge.MergeabilityBehind
	case "DIRTY":
		return forge.MergeabilityConflicting
	}

	return forge.MergeabilityUnknown
}

// messageVariables is the commit message as a merge mutation's variables, null to leave the
// message to GitHub.
func messageVariables(message *forge.CommitMessage) map[string]any {
	if message == nil {
		return map[string]any{"headline": nil, "body": nil}
	}

	return map[string]any{"headline": message.Headline, "body": message.Body}
}
