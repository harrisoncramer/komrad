package ui

import (
	"strings"

	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	"komrad/lib/xslice"
)

// pullRequestView is the pull request's number, title, author and a window on its description
// cut to rows, the whole block giving way when even the title does not fit.
func (r reviewScreen) pullRequestView(m *model, rows int) string {
	pullRequest, ok := r.currentPullRequest(m)
	if !ok || rows == 0 {
		return ""
	}

	width := lineWidthOf(m.paneWidth())
	head := r.pullRequestHeadView(m, pullRequest, width)
	if rows > 0 && paint.CountLines(head)+1 > rows {
		return ""
	}

	body := xslice.Map(pullRequestBodyRows(pullRequest.Body, width), func(row string) string {
		return paint.Clip(row, width)
	})
	shown := paint.WindowRows(body, countPullRequestWindow(head, rows), func(visible int) int {
		return max(min(r.pullRequestTop, len(body)-visible), 0)
	}, dimStyle())

	var out strings.Builder
	out.WriteString(head)
	if shown != "" {
		out.WriteString("\n" + shown)
	}

	return out.String() + "\n"
}

// pullRequestHeadView is the pull request's number, title and author, the title lit while the
// block holds the keys.
func (r reviewScreen) pullRequestHeadView(m *model, pullRequest panel.PullRequest, width int) string {
	title := paint.Panel.Title
	if r.isPullRequestFocused(m) {
		lit := paint.Panel.Style(paint.TonePrimary).Bold(true)
		title = func(row string) string { return lit.Render(row) }
	}

	var out strings.Builder
	for _, row := range paint.WrapRows(pullRequest.Ref()+" "+pullRequest.Title, width) {
		out.WriteString(title(row) + "\n")
	}
	if pullRequest.Author != "" {
		out.WriteString(dimStyle().Render(paint.Clip("@"+pullRequest.Author, width)) + "\n")
	}

	return out.String()
}

// countPullRequestWindow is the rows the description may take under head in a block of rows.
func countPullRequestWindow(head string, rows int) int {
	if rows == paint.Unbounded {
		return pullRequestWindowRows
	}

	return max(min(pullRequestWindowRows, rows-paint.CountLines(head)-2), 0)
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
