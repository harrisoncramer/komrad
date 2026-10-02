package ui

import (
	"fmt"
	"slices"
	"strings"

	"komrad/config"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	tea "charm.land/bubbletea/v2"
)

// pullRequestWindowRows is the most rows the description takes before it scrolls.
const pullRequestWindowRows = 10

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

	body := windowBodyRows(pullRequestBodyRows(pullRequest.Body, width), r.pullRequestTop, countPullRequestWindow(head, rows))

	var out strings.Builder
	out.WriteString(head)
	if len(body) > 0 {
		out.WriteString("\n")
		for _, row := range body {
			out.WriteString(dimStyle().Render(paint.Clip(row, width)) + "\n")
		}
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
	if rows < 0 {
		return pullRequestWindowRows
	}

	return min(pullRequestWindowRows, rows-paint.CountLines(head)-2)
}

// countPullRequestTopMax is the furthest a description of total rows scrolls in a window.
func countPullRequestTopMax(total, window int) int {
	if total <= window || window < 2 {
		return 0
	}

	return total - window + 1
}

// windowBodyRows is the description from top in a window, closed by where it stands when it does
// not fit whole.
func windowBodyRows(body []string, top, window int) []string {
	if len(body) <= window {
		return body
	}
	if window < 2 {
		return nil
	}

	top = max(min(top, countPullRequestTopMax(len(body), window)), 0)
	shown := slices.Clone(body[top : top+window-1])

	return append(shown, fmt.Sprintf("rows %d-%d of %d", top+1, top+window-1, len(body)))
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

func (r *reviewScreen) isPullRequestDrawn(m *model) bool {
	if _, ok := m.screenColumnSplit(); !ok {
		return false
	}

	_, ok := r.currentPullRequest(m)

	return ok
}

func (r *reviewScreen) isPullRequestFocused(m *model) bool {
	return r.focus == sidebarFocusPullRequest && m.isListFocused() && !r.isWriting() && r.isPullRequestDrawn(m)
}

func (r *reviewScreen) focusPullRequest(*model) {
	r.focus = sidebarFocusPullRequest
}

func (r *reviewScreen) blurPullRequest() {
	if r.focus == sidebarFocusPullRequest {
		r.focus = sidebarFocusDiff
	}
}

// scrollPullRequest moves the description's window by rows, held inside what it has to show.
func (r *reviewScreen) scrollPullRequest(m *model, rows int) {
	pullRequest, ok := r.currentPullRequest(m)
	if !ok {
		return
	}

	width := lineWidthOf(m.paneWidth())
	window := countPullRequestWindow(r.pullRequestHeadView(m, pullRequest, width), r.layoutSidebar(m).pullRequestRows)
	top := countPullRequestTopMax(len(pullRequestBodyRows(pullRequest.Body, width)), window)
	r.pullRequestTop = max(min(min(r.pullRequestTop, top)+rows, top), 0)
}

// pullRequestActions is the keys on the pull request, which scroll its description.
func (r *reviewScreen) pullRequestActions() screenActionSpec {
	return screenActionSpec{
		table: []screenAction{
			{
				id:   actionMove,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.Up, k.Nav.Down) },
				nav: func(m *model, pressed string) tea.Cmd {
					r.scrollPullRequest(m, directionOf(pressed, m.keys.Nav.Up))

					return nil
				},
			},
			{
				id:   actionNavPage,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.PageUp, k.Nav.PageDown) },
				nav: func(m *model, pressed string) tea.Cmd {
					r.scrollPullRequest(m, directionOf(pressed, m.keys.Nav.PageUp)*pullRequestWindowRows/2)

					return nil
				},
			},
			{
				id:   actionReviewBack,
				keys: func(k config.Keybindings) []string { return k.Review.Back },
				nav: func(*model, string) tea.Cmd {
					r.focus = sidebarFocusDiff

					return nil
				},
			},
			{
				id:   actionQuit,
				keys: func(k config.Keybindings) []string { return k.Review.Quit },
				nav:  func(m *model, _ string) tea.Cmd { return m.hide() },
			},
		},
		legend: []screenLegendRow{
			{
				of:    []actionID{actionMove},
				desc:  func(*model) string { return "scroll the description" },
				label: moveLabel,
				strip: true,
			},
			{
				of:    []actionID{actionNavPage},
				desc:  func(*model) string { return "page" },
				label: navPageLabel,
			},
			{
				of:    []actionID{actionReviewBack},
				desc:  func(*model) string { return "back to the diff" },
				strip: true,
			},
		},
	}
}
