package github

import (
	"context"
	_ "embed"

	"komrad/internal/forge"
	forgev1 "komrad/proto/gen/forge/v1"
)

//go:embed drafttarget.gql
var draftTargetQuery string

//go:embed converttodraft.gql
var convertToDraftMutation string

//go:embed markready.gql
var markReadyMutation string

// SetDraft converts the pull request to a draft or marks it ready for review, asking nothing
// of GitHub when it already stands that way.
func (f *Forge) SetDraft(ctx context.Context, address string, draft bool) error {
	owner, name, number, err := parsePullURL(address)
	if err != nil {
		return err
	}

	var data struct {
		Repository *struct {
			PullRequest *struct {
				ID      string `json:"id"`
				State   string `json:"state"`
				IsDraft bool   `json:"isDraft"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}
	answer, err := f.client.Query(ctx, draftTargetQuery, map[string]any{
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
	target := data.Repository.PullRequest
	shouldChange, err := forge.ShouldChange(forge.ShouldChangeParams{
		Sentinel:  forge.ErrNotDraftable,
		Forge:     forgev1.Kind_KIND_GITHUB,
		Ref:       forge.Ref(forgev1.Kind_KIND_GITHUB, number),
		Merged:    target.State == "MERGED",
		Closed:    target.State == "CLOSED",
		IsSettled: target.IsDraft == draft,
	})
	if err != nil || !shouldChange {
		return err
	}

	mutation := markReadyMutation
	if draft {
		mutation = convertToDraftMutation
	}

	var changed struct{}
	answer, err = f.client.Query(ctx, mutation, map[string]any{"id": target.ID}, &changed)
	if err != nil {
		return err
	}

	return mutationRefusal{sentinel: forge.ErrNotDraftable, verb: "change"}.explain(answer, address, number)
}
