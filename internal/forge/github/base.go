package github

import (
	"context"
	_ "embed"

	"komrad/internal/forge"
	forgev1 "komrad/proto/gen/forge/v1"
)

//go:embed basetarget.gql
var baseTargetQuery string

//go:embed setbase.gql
var setBaseMutation string

// SetBase points the pull request at another base branch, asking nothing of GitHub when it
// already targets it.
func (f *Forge) SetBase(ctx context.Context, address, base string) error {
	owner, name, number, err := parsePullURL(address)
	if err != nil {
		return err
	}

	var data struct {
		Repository *struct {
			PullRequest *struct {
				ID          string `json:"id"`
				State       string `json:"state"`
				BaseRefName string `json:"baseRefName"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}
	answer, err := f.client.Query(ctx, baseTargetQuery, map[string]any{
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
		Sentinel:  forge.ErrNotRetargetable,
		Forge:     forgev1.Kind_KIND_GITHUB,
		Ref:       forge.Ref(forgev1.Kind_KIND_GITHUB, number),
		Merged:    target.State == "MERGED",
		Closed:    target.State == "CLOSED",
		IsSettled: target.BaseRefName == base,
	})
	if err != nil || !shouldChange {
		return err
	}

	var changed struct{}
	answer, err = f.client.Query(ctx, setBaseMutation, map[string]any{"id": target.ID, "base": base}, &changed)
	if err != nil {
		return err
	}

	return mutationRefusal{sentinel: forge.ErrNotRetargetable, verb: "re-target"}.explain(answer, address, number)
}
