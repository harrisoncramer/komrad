package gitlab

import (
	"context"
	_ "embed"
	"fmt"
	"strconv"
	"strings"

	"komrad/internal/forge"
)

//go:embed setdraft.gql
var setDraftMutation string

// SetDraft marks the merge request a draft or ready, asking nothing of GitLab when it already
// stands that way.
func (f *Forge) SetDraft(ctx context.Context, address string, draft bool) error {
	path, number, err := parseMergeRequestURL(address)
	if err != nil {
		return err
	}

	target, err := f.readMergeRequest(ctx, address)
	if err != nil {
		return err
	}
	shouldChange, err := forge.ShouldChange(forge.ShouldChangeParams{
		Sentinel:  forge.ErrNotDraftable,
		Forge:     forge.KindGitLab,
		Ref:       forge.Ref(forge.KindGitLab, number),
		Merged:    target.MergedAt != nil,
		Closed:    target.ClosedAt != nil,
		IsSettled: target.Draft == draft,
	})
	if err != nil || !shouldChange {
		return err
	}

	var data struct {
		MergeRequestSetDraft *struct {
			Errors []string `json:"errors"`
		} `json:"mergeRequestSetDraft"`
	}
	answer, err := f.client.Query(ctx, setDraftMutation, map[string]any{
		"path":  path,
		"iid":   strconv.Itoa(number),
		"draft": draft,
	}, &data)
	if err != nil {
		return err
	}
	if data.MergeRequestSetDraft == nil {
		if err := failureOf(answer); err != nil {
			return err
		}

		return fmt.Errorf("%w: gitlab has no merge request %d in %s", forge.ErrNotFound, number, path)
	}
	if said := data.MergeRequestSetDraft.Errors; len(said) > 0 {
		return forge.ErrNotDraftable.Saying("gitlab will not change !%d: %s", number, strings.Join(said, "; "))
	}

	return failureOf(answer)
}
