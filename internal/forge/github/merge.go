package github

import (
	"context"
	_ "embed"

	"komrad/internal/forge"
	"komrad/lib/xptr"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
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
	if err := finishedRefusal(data.Repository.PullRequest.State, number); err != nil {
		return err
	}

	method, err := data.Repository.methodFor(data.Repository.PullRequest.BaseRef, params.Method)
	if err != nil {
		return err
	}

	variables := messageVariables(params.Headline, params.Body)
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

// finishedRefusal is why GitHub will not merge a pull request that has finished, nil for an open one.
func finishedRefusal(state string, number int) error {
	switch state {
	case "MERGED":
		return forge.ErrNotMergeable.Saying("github will not merge #%d: it has already merged", number)
	case "CLOSED":
		return forge.ErrNotMergeable.Saying("github will not merge #%d: it was closed without merging", number)
	}

	return nil
}

// mergeabilityOf is GitHub's mergeStateStatus in komrad's words.
func mergeabilityOf(status string) pullrequestv1.Mergeability {
	switch status {
	case "CLEAN", "HAS_HOOKS":
		return pullrequestv1.Mergeability_MERGEABILITY_CLEAN
	case "UNSTABLE":
		return pullrequestv1.Mergeability_MERGEABILITY_UNSTABLE
	case "BLOCKED", "DRAFT":
		return pullrequestv1.Mergeability_MERGEABILITY_BLOCKED
	case "BEHIND":
		return pullrequestv1.Mergeability_MERGEABILITY_BEHIND
	case "DIRTY":
		return pullrequestv1.Mergeability_MERGEABILITY_CONFLICTING
	}

	return pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN
}

// messageVariables is the commit message as a merge mutation's variables, each null to leave
// that part to GitHub.
func messageVariables(headline, body *string) map[string]any {
	variables := map[string]any{"headline": nil, "body": nil}
	if headline != nil {
		variables["headline"] = *headline
	}
	if body != nil {
		variables["body"] = *body
	}

	return variables
}
