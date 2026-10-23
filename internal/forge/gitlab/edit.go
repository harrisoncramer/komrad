package gitlab

import (
	"context"
	_ "embed"
	"fmt"
	"strconv"
	"strings"

	"komrad/internal/forge"
	forgev1 "komrad/proto/gen/forge/v1"
)

//go:embed edittarget.gql
var editTargetQuery string

//go:embed updatemergerequest.gql
var updateMergeRequestMutation string

// ReadText is the merge request's title and description, refused when the credential may not
// change them.
func (f *Forge) ReadText(ctx context.Context, address string) (*forge.Text, error) {
	target, err := f.readEditTarget(ctx, address)
	if err != nil {
		return nil, err
	}
	if err := target.Refusal(); err != nil {
		return nil, err
	}

	return &target.Text, nil
}

// Edit changes the merge request's title or description, asking nothing of GitLab when neither
// would change.
func (f *Forge) Edit(ctx context.Context, params forge.EditParams) error {
	path, number, err := parseMergeRequestURL(params.URL)
	if err != nil {
		return err
	}

	target, err := f.readEditTarget(ctx, params.URL)
	if err != nil {
		return err
	}
	changes, err := params.Changes(*target)
	if err != nil || !changes {
		return err
	}

	variables := map[string]any{"path": path, "iid": strconv.Itoa(number)}
	if params.Title != nil {
		variables["title"] = *params.Title
	}
	if params.Body != nil {
		variables["description"] = *params.Body
	}

	var data struct {
		MergeRequestUpdate *struct {
			Errors []string `json:"errors"`
		} `json:"mergeRequestUpdate"`
	}
	answer, err := f.client.Query(ctx, updateMergeRequestMutation, variables, &data)
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
		return forge.ErrNotEditable.Saying("gitlab will not edit !%d: %s", number, strings.Join(said, "; "))
	}

	return failureOf(answer)
}

func (f *Forge) readEditTarget(ctx context.Context, address string) (*forge.TextTarget, error) {
	path, number, err := parseMergeRequestURL(address)
	if err != nil {
		return nil, err
	}

	var data struct {
		Project *struct {
			MergeRequest *struct {
				Title           string `json:"title"`
				Description     string `json:"description"`
				UserPermissions struct {
					UpdateMergeRequest bool `json:"updateMergeRequest"`
				} `json:"userPermissions"`
			} `json:"mergeRequest"`
		} `json:"project"`
	}
	answer, err := f.client.Query(ctx, editTargetQuery, map[string]any{"path": path, "iid": strconv.Itoa(number)}, &data)
	if err != nil {
		return nil, err
	}
	if err := failureOf(answer); err != nil {
		return nil, err
	}
	if data.Project == nil || data.Project.MergeRequest == nil {
		return nil, noMergeRequest(number, path)
	}
	read := data.Project.MergeRequest

	return &forge.TextTarget{
		Forge:   forgev1.Kind_KIND_GITLAB,
		Ref:     forge.Ref(forgev1.Kind_KIND_GITLAB, number),
		Text:    forge.Text{Title: read.Title, Body: read.Description},
		CanEdit: read.UserPermissions.UpdateMergeRequest,
	}, nil
}
