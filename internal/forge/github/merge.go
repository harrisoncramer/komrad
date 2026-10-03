package github

import (
	"context"

	"komrad/internal/forge"
)

const mergeTargetQuery = `query MergeTarget($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) { viewerDefaultMergeMethod pullRequest(number: $number) { id state } }
}`

const mergeMutation = `mutation Merge($id: ID!, $method: PullRequestMergeMethod!) {
  mergePullRequest(input: {pullRequestId: $id, mergeMethod: $method}) { clientMutationId }
}`

// Merge lands the pull request with the method GitHub offers the credential by default.
func (f *Forge) Merge(ctx context.Context, address string) error {
	owner, name, number, err := parsePullURL(address)
	if err != nil {
		return err
	}

	var data struct {
		Repository *struct {
			ViewerDefaultMergeMethod string `json:"viewerDefaultMergeMethod"`
			PullRequest              *struct {
				ID    string `json:"id"`
				State string `json:"state"`
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

	var merged struct{}
	answer, err = f.client.Query(ctx, mergeMutation, map[string]any{
		"id":     data.Repository.PullRequest.ID,
		"method": data.Repository.ViewerDefaultMergeMethod,
	}, &merged)
	if err != nil {
		return err
	}

	return mutationRefusal{sentinel: forge.ErrNotMergeable, verb: "merge"}.explain(answer, address, number)
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
