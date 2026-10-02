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
	pullRequest, ok := r.currentPullRequest(m)
	if !ok || rows == 0 {
		return ""
	}

	width := lineWidthOf(m.paneWidth())
	var out strings.Builder
	for _, row := range paint.WrapRows(pullRequest.Ref()+" "+pullRequest.Title, width) {
		out.WriteString(paint.Panel.Title(row) + "\n")
	}
	if pullRequest.Author != "" {
		out.WriteString(dimStyle().Render(paint.Clip("@"+pullRequest.Author, width)) + "\n")
	}

	head := paint.CountLines(out.String())
	if rows > 0 && head+1 > rows {
		return ""
	}

	body := pullRequestBodyRows(pullRequest.Body, width)
	if rows > 0 {
		body = cutBodyRows(body, rows-head-2, width)
	}
	if len(body) > 0 {
		out.WriteString("\n")
		for _, row := range body {
			out.WriteString(dimStyle().Render(row) + "\n")
		}
	}

	return out.String() + "\n"
}

func cutBodyRows(body []string, rows, width int) []string {
	if rows <= 0 {
		return nil
	}
	if len(body) <= rows {
		return body
	}

	body = body[:rows]
	body[rows-1] = paint.Clip(body[rows-1]+"…", width)

	return body
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
