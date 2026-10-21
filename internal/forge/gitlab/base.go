package gitlab

import (
	"context"
	_ "embed"
	"fmt"
	"strconv"
	"strings"

	"komrad/internal/forge"
)

//go:embed setbase.gql
var setBaseMutation string

// SetBase points the merge request at another target branch, asking nothing of GitLab when it
// already targets it.
func (f *Forge) SetBase(ctx context.Context, address, base string) error {
	path, number, err := parseMergeRequestURL(address)
	if err != nil {
		return err
	}

	target, err := f.readMergeRequest(ctx, address)
	if err != nil {
		return err
	}
	shouldChange, err := forge.ShouldChange(forge.ShouldChangeParams{
		Sentinel:  forge.ErrNotRetargetable,
		Forge:     forge.KindGitLab,
		Ref:       forge.Ref(forge.KindGitLab, number),
		Merged:    target.MergedAt != nil,
		Closed:    target.ClosedAt != nil,
		IsSettled: target.BaseBranch == base,
	})
	if err != nil || !shouldChange {
		return err
	}

	var data struct {
		MergeRequestUpdate *struct {
			Errors []string `json:"errors"`
		} `json:"mergeRequestUpdate"`
	}
	answer, err := f.client.Query(ctx, setBaseMutation, map[string]any{
		"path": path,
		"iid":  strconv.Itoa(number),
		"base": base,
	}, &data)
	if err != nil {
		return err
	}
	if data.MergeRequestUpdate == nil {
		if err := failureOf(answer); err != nil {
			return err
		}

		return fmt.Errorf("%w: gitlab has no merge request %d in %s", forge.ErrNotFound, number, path)
	}
	if said := data.MergeRequestUpdate.Errors; len(said) > 0 {
		return forge.ErrNotRetargetable.Saying("gitlab will not re-target !%d: %s", number, strings.Join(said, "; "))
	}

	return failureOf(answer)
}
