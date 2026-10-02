package ui

import (
	"fmt"
	"math"
	"strings"

	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
)

// currentPullRequest is the pull request open from the branch under review, as the list holds it,
// once its title has been read from the forge.
func (r reviewScreen) currentPullRequest(m *model) (panel.PullRequest, bool) {
	item, ok := m.listing.anyAgent(r.agentID)
	if !ok || len(item.PullRequests) == 0 || item.PullRequest.State == pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE {
		return panel.PullRequest{}, false
	}
	if item.PullRequests[0].Title == "" {
		return panel.PullRequest{}, false
	}

	return item.PullRequests[0], true
}

// pullRequestView is the pull request's number, title, author and description cut to rows, the
// description giving way first and the whole block when even the title does not fit.
func (r reviewScreen) pullRequestView(m *model, rows int) string {
	if rows == paint.Unbounded {
		rows = math.MaxInt
	}

	pullRequest, ok := r.currentPullRequest(m)
	if !ok || rows <= 0 {
		return ""
	}

	width := lineWidthOf(m.paneWidth())
	head := paint.WrapRows(fmt.Sprintf("#%d %s", pullRequest.Number, pullRequest.Title), width)
	if pullRequest.Author != "" {
		head = append(head, "@"+pullRequest.Author)
	}

	if len(head)+1 > rows {
		return ""
	}

	var out strings.Builder
	for at, row := range head {
		if at == len(head)-1 && pullRequest.Author != "" {
			out.WriteString(dimStyle().Render(row) + "\n")

			continue
		}
		out.WriteString(paint.Panel.Title(row) + "\n")
	}

	body := pullRequestBodyRows(pullRequest.Body, width)
	room := rows - len(head) - 2
	if len(body) == 0 || room <= 0 {
		return out.String() + "\n"
	}

	if len(body) > room {
		body = body[:room]
		body[room-1] = paint.Clip(body[room-1]+"…", width)
	}

	out.WriteString("\n")
	for _, row := range body {
		out.WriteString(dimStyle().Render(row) + "\n")
	}

	return out.String() + "\n"
}

// pullRequestBodyRows is a description wrapped to width, its runs of blank lines kept to one
// and none at either end.
func pullRequestBodyRows(body string, width int) []string {
	var rows []string
	blank := false
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		line = strings.TrimRight(line, " \t")
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
