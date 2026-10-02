package ui

import (
	"strings"

	"komrad/config"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	"komrad/lib/xslice"
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

	body := xslice.Map(pullRequestBodyRows(pullRequest.Body, width), func(row string) string {
		return dimStyle().Render(paint.Clip(row, width))
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

func (r *reviewScreen) isPullRequestDrawn(m *model) bool {
	if _, ok := m.screenColumnSplit(); !ok {
		return false
	}

	_, ok := r.currentPullRequest(m)

	return ok
}

func (r *reviewScreen) isPullRequestFocused(m *model) bool {
	return r.focus == reviewFocusPullRequest && m.isListFocused() && !r.isWriting() && r.isPullRequestDrawn(m)
}

func (r *reviewScreen) focusPullRequest(*model) {
	r.focus = reviewFocusPullRequest
}

func (r *reviewScreen) blurPullRequest() {
	if r.focus == reviewFocusPullRequest {
		r.focus = reviewFocusDiff
	}
}

// scrollPullRequest moves the description's window by rows, held inside what it has to show.
func (r *reviewScreen) scrollPullRequest(m *model, rows int) {
	topMax, ok := r.pullRequestTopMax(m)
	if !ok {
		return
	}

	from := min(r.pullRequestTop, topMax)
	r.pullRequestTop = max(min(from+rows, topMax), 0)
}

// pullRequestTopMax is the furthest the description's window can scroll, and false with no pull
// request to show.
func (r *reviewScreen) pullRequestTopMax(m *model) (int, bool) {
	pullRequest, ok := r.currentPullRequest(m)
	if !ok {
		return 0, false
	}

	width := lineWidthOf(m.paneWidth())
	window := countPullRequestWindow(r.pullRequestHeadView(m, pullRequest, width), r.layoutSidebar(m).pullRequestRows)

	total := len(pullRequestBodyRows(pullRequest.Body, width))

	return paint.WindowOf(total, window, func(visible int) int { return total - visible }).Start, true
}

// pullRequestNav is the movement keys scrolling the pull request's description.
type pullRequestNav struct{ r *reviewScreen }

func (p pullRequestNav) moveBy(m *model, rows int) tea.Cmd {
	p.r.scrollPullRequest(m, rows)

	return nil
}

func (p pullRequestNav) moveToEnd(m *model, direction int) tea.Cmd {
	topMax, ok := p.r.pullRequestTopMax(m)
	if !ok {
		return nil
	}

	p.r.pullRequestTop = 0
	if direction > 0 {
		p.r.pullRequestTop = topMax
	}

	return nil
}

// pullRequestActions is the keys on the pull request, which scroll its description.
func (r *reviewScreen) pullRequestActions() screenActionSpec {
	return screenActionSpec{
		table: append(navActions(pullRequestNav{r}, navSteps{page: pullRequestWindowRows / 2, scroll: 1}), []screenAction{
			{
				id:   actionReviewBack,
				keys: func(k config.Keybindings) []string { return k.Review.Back },
				nav: func(*model, string) tea.Cmd {
					r.blurPullRequest()

					return nil
				},
			},
			{
				id:   actionQuit,
				keys: func(k config.Keybindings) []string { return k.Review.Quit },
				nav:  func(m *model, _ string) tea.Cmd { return m.hide() },
			},
		}...),
		legend: []screenLegendRow{
			navMoveRow("scroll the description"),
			navPageRow(),
			navJumpRow(),
			{
				of:    []actionID{actionReviewBack},
				desc:  func(*model) string { return "back to the diff" },
				strip: true,
			},
		},
	}
}
