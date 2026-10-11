package ui

import (
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

func (r *reviewScreen) isPullRequestRendered(m *model) bool {
	if _, ok := m.screenColumnSplit(); !ok {
		return false
	}

	_, ok := r.currentPullRequest(m)

	return ok
}

func (r *reviewScreen) isPullRequestFocused(m *model) bool {
	return r.focus == reviewFocusPullRequest && m.isListFocused() && !r.isWriting() && r.isPullRequestRendered(m)
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
				keys: fixedBack,
				nav: func(*model, string) tea.Cmd {
					r.blurPullRequest()

					return nil
				},
			},
			{
				id:   actionQuit,
				keys: fixedQuit,
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
