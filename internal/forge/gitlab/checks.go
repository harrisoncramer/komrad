package gitlab

import (
	"net/url"
	"strings"
	"time"

	"komrad/internal/forge"
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
	case "MERGED_RESULT", "MERGE_TRAIN":
		return true
	}

	return p.Sha == sha
}

// listing is the head pipeline as the one required check on the merge request, none for a pipeline
// on another commit or one that says nothing about the branch.
func (n mergeRequestNode) listing() *forge.CheckListing {
	listing := &forge.CheckListing{}
	if !n.HeadPipeline.isOn(n.DiffHeadSha) {
		return listing
	}

	pipeline := n.HeadPipeline
	check := forge.Check{
		ProviderID: strings.TrimPrefix(pipeline.ID, "gid://gitlab/Ci::Pipeline/"),
		Kind:       forge.CheckKindPipeline,
		Name:       "pipeline",
		StartedAt:  pipeline.StartedAt,
		Required:   true,
		URL:        n.pageOf(pipeline.Path),
	}
	switch pipeline.Status {
	case "SUCCESS", "FAILED", "CANCELED", "CANCELING":
		check.State = forge.CheckStateCompleted
		check.Conclusion = strings.ToLower(pipeline.Status)
		check.FinishedAt = pipeline.FinishedAt
	case "RUNNING", "WAITING_FOR_CALLBACK":
		check.State = forge.CheckStateRunning
	case "CREATED", "WAITING_FOR_RESOURCE", "PREPARING", "PENDING", "SCHEDULED":
		check.State = forge.CheckStateQueued
	default:
		return listing
	}
	listing.Checks = []forge.Check{check}

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
