package gitlab

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"komrad/internal/forge"
	"komrad/lib/xhttp"
)

//go:embed mergeplan.gql
var mergePlanQuery string

// planTarget is a merge request about to merge, with the messages GitLab would write for it.
type planTarget struct {
	State                      string `json:"state"`
	DiffHeadSha                string `json:"diffHeadSha"`
	SquashReadOnly             bool   `json:"squashReadOnly"`
	SquashOnMerge              bool   `json:"squashOnMerge"`
	DefaultMergeCommitMessage  string `json:"defaultMergeCommitMessage"`
	DefaultSquashCommitMessage string `json:"defaultSquashCommitMessage"`
}

// PlanMerge reads the project's own merge method and whether the merge request may squash, with
// the message GitLab would write for each.
func (f *Forge) PlanMerge(ctx context.Context, address string) (*forge.MergePlan, error) {
	path, number, err := parseMergeRequestURL(address)
	if err != nil {
		return nil, err
	}

	var data struct {
		Project *struct {
			MergeRequest *planTarget `json:"mergeRequest"`
		} `json:"project"`
	}
	answer, err := f.client.Query(ctx, mergePlanQuery, map[string]any{"path": path, "iid": strconv.Itoa(number)}, &data)
	if err != nil {
		return nil, err
	}
	if err := failureOf(answer); err != nil {
		return nil, err
	}
	if data.Project == nil || data.Project.MergeRequest == nil {
		return nil, noMergeRequest(number, path)
	}

	target := data.Project.MergeRequest
	switch target.State {
	case "merged":
		return nil, forge.ErrNotMergeable.Saying("gitlab will not merge !%d: it has already merged", number)
	case "closed":
		return nil, forge.ErrNotMergeable.Saying("gitlab will not merge !%d: it was closed without merging", number)
	}

	own, err := f.projectMergeMethod(ctx, path)
	if err != nil {
		return nil, err
	}
	ownOption := forge.MergeOption{
		Method:  own,
		Allowed: !target.SquashReadOnly || !target.SquashOnMerge,
	}
	if own == forge.MergeMethodMerge {
		ownOption.Message = splitMessage(target.DefaultMergeCommitMessage)
	}

	plan := &forge.MergePlan{
		Options: []forge.MergeOption{
			ownOption,
			{
				Method:  forge.MergeMethodSquash,
				Allowed: !target.SquashReadOnly || target.SquashOnMerge,
				Message: splitMessage(target.DefaultSquashCommitMessage),
			},
		},
		Pick:    own,
		HeadSHA: target.DiffHeadSha,
	}
	if target.SquashOnMerge {
		plan.Pick = forge.MergeMethodSquash
	}

	return plan, nil
}

// projectMergeMethod is how the project merges when it does not squash, read over REST since
// GitLab's GraphQL does not say.
func (f *Forge) projectMergeMethod(ctx context.Context, path string) (forge.MergeMethod, error) {
	var project struct {
		MergeMethod string `json:"merge_method"`
	}
	answer, err := f.client.Send(ctx, xhttp.JSONParams{
		Method: http.MethodGet,
		URL:    fmt.Sprintf("%s/projects/%s", f.client.API(), url.PathEscape(path)),
	}, &project)
	if errors.Is(err, forge.ErrThrottled) {
		return "", err
	}

	switch answer.Status {
	case http.StatusNotFound:
		return "", fmt.Errorf("%w: gitlab has no project %s, which is also its answer for one the credential cannot see", forge.ErrNotFound, path)
	case http.StatusForbidden:
		return "", forge.ErrUnauthorized.Saying("gitlab will not let the credential read %s: %v", path, err)
	}
	if err != nil {
		return "", err
	}
	if project.MergeMethod == "ff" {
		return forge.MergeMethodFastForward, nil
	}

	return forge.MergeMethodMerge, nil
}

// splitMessage is a commit message split at its first line, nil for none.
func splitMessage(message string) *forge.CommitMessage {
	if message == "" {
		return nil
	}

	headline, body, _ := strings.Cut(message, "\n")

	return &forge.CommitMessage{Headline: headline, Body: strings.TrimLeft(body, "\n")}
}

// joinMessage is a commit message as one text, its headline apart from its body by a blank line.
func joinMessage(message forge.CommitMessage) string {
	if message.Body == "" {
		return message.Headline
	}

	return message.Headline + "\n\n" + message.Body
}

// mergeBody is the REST fields that say how a merge request merges, empty to leave it to the
// project.
func mergeBody(method forge.MergeMethod, message *forge.CommitMessage, headSHA string) (map[string]any, error) {
	body := map[string]any{}
	switch method {
	case "":
	case forge.MergeMethodSquash:
		body["squash"] = true
	case forge.MergeMethodMerge, forge.MergeMethodFastForward:
		body["squash"] = false
	case forge.MergeMethodRebase:
		return nil, forge.ErrNoMergeMethod.Saying("gitlab cannot merge by %s", method)
	}
	if message != nil {
		field := "merge_commit_message"
		if method == forge.MergeMethodSquash {
			field = "squash_commit_message"
		}
		body[field] = joinMessage(*message)
	}
	if headSHA != "" {
		body["sha"] = headSHA
	}

	return body, nil
}
