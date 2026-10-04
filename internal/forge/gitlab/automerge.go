package gitlab

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"komrad/internal/forge"
	"komrad/internal/forge/graphql"
	"komrad/lib/xhttp"
)

//go:embed automergetarget.gql
var autoMergeTargetQuery string

// SetAutoMerge asks GitLab to merge the merge request once its pipeline passes, held to the head
// the person saw, or cancels that, refusing a draft or one GitLab would merge at once.
func (f *Forge) SetAutoMerge(ctx context.Context, params forge.SetAutoMergeParams) error {
	path, number, err := parseMergeRequestURL(params.URL)
	if err != nil {
		return err
	}

	target, err := f.readAutoMergeTarget(ctx, path, number)
	if err != nil {
		return err
	}
	shouldChange, err := forge.ShouldChange(forge.ShouldChangeParams{
		Sentinel:  forge.ErrNoAutoMerge,
		Forge:     forge.KindGitLab,
		Ref:       forge.Ref(forge.KindGitLab, number),
		Merged:    target.State == "merged",
		Closed:    target.State == "closed",
		IsSettled: target.AutoMergeEnabled == params.On,
	})
	if err != nil || !shouldChange {
		return err
	}
	if params.On && target.Draft {
		return forge.ErrNoAutoMerge.Saying("gitlab will not queue !%d: it is still a draft", number)
	}
	if params.On && len(target.AvailableAutoMergeStrategies) == 0 {
		return forge.ErrNoAutoMerge.Saying("gitlab will not queue !%d: it has no running pipeline to wait for, or the credential cannot merge it", number)
	}

	return f.sendAutoMerge(ctx, sendAutoMergeParams{
		path:   path,
		number: number,
		change: params,
	})
}

// readAutoMergeTarget reads the merge request whose auto-merge is about to change.
func (f *Forge) readAutoMergeTarget(ctx context.Context, path string, number int) (*autoMergeTarget, error) {
	var data struct {
		Project *struct {
			MergeRequest *autoMergeTarget `json:"mergeRequest"`
		} `json:"project"`
	}
	answer, err := f.client.Query(ctx, autoMergeTargetQuery, map[string]any{"path": path, "iid": strconv.Itoa(number)}, &data)
	if err != nil {
		return nil, err
	}
	if err := failureOf(answer); err != nil {
		return nil, err
	}
	if data.Project == nil || data.Project.MergeRequest == nil {
		return nil, noMergeRequest(number, path)
	}

	return data.Project.MergeRequest, nil
}

// sendAutoMergeParams names the merge request and the change asked of its auto-merge.
type sendAutoMergeParams struct {
	path   string
	number int
	change forge.SetAutoMergeParams
}

// sendAutoMerge queues the merge request to merge once its pipeline passes, or cancels that,
// over GitLab's REST API.
func (f *Forge) sendAutoMerge(ctx context.Context, params sendAutoMergeParams) error {
	endpoint := fmt.Sprintf("%s/projects/%s/merge_requests/%d", f.client.API(), url.PathEscape(params.path), params.number)
	request := xhttp.JSONParams{
		Method: http.MethodPost,
		URL:    endpoint + "/cancel_merge_when_pipeline_succeeds",
	}
	if params.change.On {
		body := map[string]any{
			"auto_merge":                   true,
			"merge_when_pipeline_succeeds": true,
		}
		if params.change.HeadSHA != "" {
			body["sha"] = params.change.HeadSHA
		}
		request = xhttp.JSONParams{
			Method: http.MethodPut,
			URL:    endpoint + "/merge",
			Body:   body,
		}
	}

	sent, err := f.client.Send(ctx, request, nil)
	if errors.Is(err, forge.ErrThrottled) {
		return err
	}

	switch sent.Status {
	case http.StatusNotFound:
		return noMergeRequest(params.number, params.path)
	case http.StatusForbidden:
		return forge.ErrUnauthorized.Saying("gitlab will not let the credential change auto-merge on %s: %v", params.change.URL, err)
	case http.StatusMethodNotAllowed, http.StatusNotAcceptable, http.StatusConflict, http.StatusUnprocessableEntity:
		return forge.ErrNoAutoMerge.Saying("gitlab will not change auto-merge on !%d: %s", params.number, graphql.MessageOf(sent.Body))
	}

	return err
}

// autoMergeTarget is a merge request whose auto-merge is about to change.
type autoMergeTarget struct {
	State                        string   `json:"state"`
	Draft                        bool     `json:"draft"`
	AutoMergeEnabled             bool     `json:"autoMergeEnabled"`
	AvailableAutoMergeStrategies []string `json:"availableAutoMergeStrategies"`
}

// autoMerge is whether the merge request is set to merge on its own and whether komrad may set it.
func (n mergeRequestNode) autoMerge() forge.AutoMerge {
	return forge.AutoMerge{
		On:      n.AutoMergeEnabled,
		Allowed: !n.Draft && len(n.AvailableAutoMergeStrategies) > 0,
	}
}
