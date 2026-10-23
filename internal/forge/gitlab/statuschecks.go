package gitlab

import (
	"net/url"
	"strings"
	"time"

	"komrad/internal/forge"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
)

// The pipeline statuses GitLab documents.
const (
	pipelineCreated            = "CREATED"
	pipelineWaitingForResource = "WAITING_FOR_RESOURCE"
	pipelinePreparing          = "PREPARING"
	pipelineWaitingForCallback = "WAITING_FOR_CALLBACK"
	pipelinePending            = "PENDING"
	pipelineRunning            = "RUNNING"
	pipelineSuccess            = "SUCCESS"
	pipelineFailed             = "FAILED"
	pipelineCanceling          = "CANCELING"
	pipelineCanceled           = "CANCELED"
	pipelineSkipped            = "SKIPPED"
	pipelineManual             = "MANUAL"
	pipelineScheduled          = "SCHEDULED"
)

// The merge request events GitLab says a pipeline ran for.
const (
	pipelineEventDetached     = "DETACHED"
	pipelineEventMergedResult = "MERGED_RESULT"
	pipelineEventMergeTrain   = "MERGE_TRAIN"
)

type pipelineNode struct {
	ID                    string     `json:"id"`
	Sha                   string     `json:"sha"`
	Status                string     `json:"status"`
	MergeRequestEventType string     `json:"mergeRequestEventType"`
	Path                  string     `json:"path"`
	StartedAt             *time.Time `json:"startedAt"`
	FinishedAt            *time.Time `json:"finishedAt"`
}

// isOn says whether the pipeline ran on sha, trusting GitLab for a merged-results pipeline whose
// sha is the merge ref instead.
func (p *pipelineNode) isOn(sha string) bool {
	if p == nil {
		return false
	}

	switch p.MergeRequestEventType {
	case pipelineEventMergedResult, pipelineEventMergeTrain:
		return true
	}

	return p.Sha == sha
}

// listing is the head pipeline as the one required check on the merge request, none for a pipeline
// on another commit or one that says nothing about the branch.
func (n mergeRequestNode) listing() *forge.StatusCheckListing {
	listing := &forge.StatusCheckListing{}
	if !n.HeadPipeline.isOn(n.DiffHeadSha) {
		return listing
	}

	pipeline := n.HeadPipeline
	check := forge.StatusCheck{
		ProviderID: strings.TrimPrefix(pipeline.ID, "gid://gitlab/Ci::Pipeline/"),
		Kind:       pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_PIPELINE,
		Name:       "pipeline",
		StartedAt:  pipeline.StartedAt,
		Required:   true,
		URL:        n.pageOf(pipeline.Path),
	}
	switch pipeline.Status {
	case pipelineSuccess, pipelineFailed, pipelineCanceled, pipelineCanceling:
		check.State = pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED
		check.Conclusion = strings.ToLower(pipeline.Status)
		check.FinishedAt = pipeline.FinishedAt
	case pipelineRunning, pipelineWaitingForCallback:
		check.State = pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_RUNNING
	case pipelineCreated, pipelineWaitingForResource, pipelinePreparing, pipelinePending, pipelineScheduled:
		check.State = pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_QUEUED
	default:
		return listing
	}
	listing.StatusChecks = []forge.StatusCheck{check}

	return listing
}

// pageOf is the page at path on the merge request's GitLab host, empty when GitLab gave no path.
func (n mergeRequestNode) pageOf(path string) string {
	page, err := url.Parse(n.WebURL)
	if path == "" || err != nil {
		return ""
	}

	return page.ResolveReference(&url.URL{Path: path}).String()
}
