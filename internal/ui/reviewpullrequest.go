package ui

import (
	"strings"

	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
)

// currentPullRequest is the pull request open from the branch under review, as the list holds it,
// once its title has been read from the forge.
func (r reviewScreen) currentPullRequest(m *model) (panel.PullRequest, bool) {
	item, ok := m.listing.anyAgent(r.agentID)
	if !ok {
		return panel.PullRequest{}, false
	}

	task := m.listing.tasks.findForAgent(item.Agent)
	if len(task.PullRequests) == 0 || task.PullRequest.State == pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE {
		return panel.PullRequest{}, false
	}
	if task.PullRequests[0].Title == "" {
		return panel.PullRequest{}, false
	}

	return task.PullRequests[0], true
}

// pullRequestBodyRows is a description wrapped to width, its runs of blank lines kept to one
// and none at either end.
func pullRequestBodyRows(body string, width int) []string {
	var rows []string
	blank := false
	for line := range strings.Lines(body) {
		line = strings.TrimRight(line, " \t\r\n")
		if line == "" {
			blank = len(rows) > 0

			continue
		}
		if blank {
			rows = append(rows, "")
			blank = false
		}
		rows = append(rows, paint.WrapRows(line, width)...)
	}

	return rows
}
