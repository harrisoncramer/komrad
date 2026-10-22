package ui

import (
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
)

// currentPullRequest is the pull request open from the branch under review, as the list holds it,
// once its title has been read from the forge.
func (r reviewScreen) currentPullRequest(in screen.Inputs) (panel.PullRequest, bool) {
	item, ok := in.Listing.AnyAgent(r.agentID)
	if !ok {
		return panel.PullRequest{}, false
	}

	task := in.Listing.Tasks.FindForAgent(item.Agent)
	if len(task.PullRequests) == 0 || task.PullRequest.State == pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE {
		return panel.PullRequest{}, false
	}
	if task.PullRequests[0].Title == "" {
		return panel.PullRequest{}, false
	}

	return task.PullRequests[0], true
}
