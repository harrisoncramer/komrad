package github

import (
	"context"
	_ "embed"
	"strings"

	"komrad/internal/forge"
	forgev1 "komrad/proto/gen/forge/v1"
)

//go:embed edittarget.gql
var editTargetQuery string

//go:embed updatepullrequest.gql
var updatePullRequestMutation string

// editTarget is a pull request's text and whether the credential may change it, with the node id
// a mutation names it by.
type editTarget struct {
	forge.TextTarget
	id     string
	number int
}

// ReadText is the pull request's title and description, refused when the credential may not
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

// Edit changes the pull request's title or description, asking nothing of GitHub when neither
// would change.
func (f *Forge) Edit(ctx context.Context, params forge.EditParams) error {
	target, err := f.readEditTarget(ctx, params.URL)
	if err != nil {
		return err
	}
	changes, err := params.Changes(target.TextTarget)
	if err != nil || !changes {
		return err
	}

	variables := map[string]any{"id": target.id}
	if params.Title != nil {
		variables["title"] = *params.Title
	}
	if params.Body != nil {
		variables["body"] = *params.Body
	}

	var changed struct{}
	answer, err := f.client.Query(ctx, updatePullRequestMutation, variables, &changed)
	if err != nil {
		return err
	}

	return mutationRefusal{sentinel: forge.ErrNotEditable, verb: "edit"}.explain(answer, params.URL, target.number)
}

func (f *Forge) readEditTarget(ctx context.Context, address string) (*editTarget, error) {
	owner, name, number, err := parsePullURL(address)
	if err != nil {
		return nil, err
	}

	var data struct {
		Repository *struct {
			PullRequest *struct {
				ID              string `json:"id"`
				Title           string `json:"title"`
				Body            string `json:"body"`
				ViewerCanUpdate bool   `json:"viewerCanUpdate"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}
	answer, err := f.client.Query(ctx, editTargetQuery, map[string]any{
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
	read := data.Repository.PullRequest

	return &editTarget{
		TextTarget: forge.TextTarget{
			Forge: forgev1.Kind_KIND_GITHUB,
			Ref:   forge.Ref(forgev1.Kind_KIND_GITHUB, number),
			Text: forge.Text{
				Title: read.Title,
				// GitHub keeps the line endings a browser sent.
				Body: strings.ReplaceAll(read.Body, "\r\n", "\n"),
			},
			CanEdit: read.ViewerCanUpdate,
		},
		id:     read.ID,
		number: number,
	}, nil
}
