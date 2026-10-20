package github

import (
	"context"
	_ "embed"

	"strings"

	"komrad/internal/forge"
)

//go:embed mergetarget.gql
var mergeTargetQuery string

// Merge lands the pull request through GitHub's asynchronous merge, by the method asked for, or by one
// the repository and its base branch allow when none was.
func (f *Forge) Merge(ctx context.Context, params forge.MergeParams) error {
	owner, name, number, err := parsePullURL(params.URL)
	if err != nil {
		return err
	}

	var data struct {
		Repository *struct {
			mergeSettings
			PullRequest *struct {
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

	if err := f.refuseUnderOpen(ctx, owner, name, number); err != nil {
		return err
	}

	return f.mergeAsync(ctx, mergeAsyncParams{
		Owner:  owner,
		Name:   name,
		Number: number,
		Method: strings.ToLower(method),
		Merge:  params,
	})
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
