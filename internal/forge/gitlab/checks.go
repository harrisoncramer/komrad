package gitlab

import "komrad/internal/forge"

type pipelineNode struct {
	Sha                   string `json:"sha"`
	Status                string `json:"status"`
	MergeRequestEventType string `json:"mergeRequestEventType"`
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

// checks is the head pipeline's status in komrad's words, none for a pipeline on another commit.
func (n mergeReadinessNode) checks() forge.Checks {
	if !n.HeadPipeline.isOn(n.DiffHeadSha) {
		return forge.ChecksNone
	}

	switch n.HeadPipeline.Status {
	case "SUCCESS":
		return forge.ChecksPassing
	case "FAILED", "CANCELED", "CANCELING":
		return forge.ChecksFailing
	case "CREATED", "WAITING_FOR_RESOURCE", "PREPARING", "WAITING_FOR_CALLBACK", "PENDING", "RUNNING", "SCHEDULED":
		return forge.ChecksPending
	}

	return forge.ChecksNone
}
