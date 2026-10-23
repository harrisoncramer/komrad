package github

import (
	"context"
	_ "embed"
	"maps"

	"komrad/internal/forge"
	"komrad/lib/xptr"
	forgev1 "komrad/proto/gen/forge/v1"
)

//go:embed automergetarget.gql
var autoMergeTargetQuery string

//go:embed enableautomerge.gql
var enableAutoMergeMutation string

//go:embed disableautomerge.gql
var disableAutoMergeMutation string

// SetAutoMerge asks GitHub to merge the pull request with a method its base branch allows once
// it may, holding it to the head the person saw, or withdraws that request, asking nothing of
// GitHub when it already stands that way.
func (f *Forge) SetAutoMerge(ctx context.Context, params forge.SetAutoMergeParams) error {
	owner, name, number, err := parsePullURL(params.URL)
	if err != nil {
		return err
	}

	target, err := f.readAutoMergeTarget(ctx, owner, name, number)
	if err != nil {
		return err
	}
	shouldChange, err := forge.ShouldChange(forge.ShouldChangeParams{
		Sentinel:  forge.ErrNoAutoMerge,
		Forge:     forgev1.Kind_KIND_GITHUB,
		Ref:       forge.Ref(forgev1.Kind_KIND_GITHUB, number),
		Merged:    target.State == "MERGED",
		Closed:    target.State == "CLOSED",
		IsSettled: (target.AutoMergeRequest != nil) == params.On,
	})
	if err != nil || !shouldChange {
		return err
	}

	mutation := disableAutoMergeMutation
	variables := map[string]any{"id": target.ID}
	if params.On {
		method, err := target.settings.methodFor(target.BaseRef, params.Method)
		if err != nil {
			return err
		}

		mutation = enableAutoMergeMutation
		maps.Copy(variables, messageVariables(params.Headline, params.Body))
		variables["method"] = method
		variables["head"] = xptr.NonEmpty(params.HeadSHA)
	}

	var changed struct{}
	answer, err := f.client.Query(ctx, mutation, variables, &changed)
	if err != nil {
		return err
	}

	return mutationRefusal{sentinel: forge.ErrNoAutoMerge, verb: "change auto-merge on"}.explain(answer, params.URL, number)
}

// readAutoMergeTarget reads the pull request whose auto-merge is about to change.
func (f *Forge) readAutoMergeTarget(ctx context.Context, owner, name string, number int) (*autoMergeTarget, error) {
	var data struct {
		Repository *struct {
			mergeSettings
			PullRequest *autoMergeTarget `json:"pullRequest"`
		} `json:"repository"`
	}
	answer, err := f.client.Query(ctx, autoMergeTargetQuery, map[string]any{
		"owner":  owner,
		"name":   name,
		"number": number,
	}, &data)
	if err != nil {
		return nil, err
	}
	if err := failureOf(answer); err != nil {
		return nil, err
	}
	if data.Repository == nil || data.Repository.PullRequest == nil {
		return nil, noPullRequest(number, owner, name)
	}

	target := data.Repository.PullRequest
	target.settings = data.Repository.mergeSettings

	return target, nil
}

// autoMergeTarget is a pull request whose auto-merge is about to change, with the merge methods
// its repository allows.
type autoMergeTarget struct {
	ID               string    `json:"id"`
	State            string    `json:"state"`
	BaseRef          *baseRef  `json:"baseRef"`
	AutoMergeRequest *struct{} `json:"autoMergeRequest"`
	settings         mergeSettings
}

// autoMerge is whether the pull request carries an auto-merge request and whether the
// credential may make one, which GitHub refuses on a draft, a pull request that can merge now
// and a repository that does not allow it.
func (n mergeReadinessNode) autoMerge() forge.AutoMerge {
	return forge.AutoMerge{
		On:      n.AutoMergeRequest != nil,
		Allowed: n.ViewerCanEnableAutoMerge,
	}
}
