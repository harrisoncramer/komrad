package ui

import (
	"weave/config"

	tea "github.com/charmbracelet/bubbletea"
)

// reviewActions is the review screen as one table, walked in precedence order by the dispatch and
// named in reading order by a legend of the same ids.
func reviewActions() actionSpec {
	return actionSpec{
		table:  reviewTable(),
		legend: reviewLegend(),
	}
}

func reviewTable() []action {
	return []action{
		{
			id:   actionReviewBack,
			keys: func(k config.Keybindings) []string { return k.Review.Back },
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.leaveReview() },
		},
		{
			id:   actionQuit,
			keys: func(k config.Keybindings) []string { return k.Review.Quit },
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m, m.hide() },
		},
		{
			id:   actionMove,
			keys: func(k config.Keybindings) []string { return concatKeys(k.Nav.Up, k.Nav.Down) },
			run:  model.moveReview,
		},
		{
			id:   actionJump,
			keys: func(k config.Keybindings) []string { return concatKeys(k.Nav.Top, k.Nav.Bottom) },
			run:  model.jumpReview,
		},
		{
			id:   actionReviewScroll,
			keys: func(k config.Keybindings) []string { return concatKeys(k.Review.ScrollDown, k.Review.ScrollUp) },
			run:  model.scrollReviewBy,
		},
		{
			id:   actionReviewFile,
			keys: func(k config.Keybindings) []string { return concatKeys(k.Review.NextFile, k.Review.PrevFile) },
			run:  model.stepReviewFile,
		},
		{
			id:   actionReviewThread,
			keys: func(k config.Keybindings) []string { return concatKeys(k.Review.NextThread, k.Review.PrevThread) },
			run:  model.stepReviewThread,
		},
		{
			id:   actionFilterOpen,
			keys: func(k config.Keybindings) []string { return k.Filter.Open },
			run:  model.openReviewFilter,
		},
		{
			id:   actionReviewMatch,
			keys: func(k config.Keybindings) []string { return concatKeys(k.Review.NextMatch, k.Review.PrevMatch) },
			run:  model.stepReviewMatch,
		},
		{
			id:   actionReviewPan,
			keys: func(k config.Keybindings) []string { return concatKeys(k.Review.PanLeft, k.Review.PanRight) },
			run:  model.panReviewBy,
		},
		{
			id:   actionReviewPanReset,
			keys: func(k config.Keybindings) []string { return k.Review.PanReset },
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.panReview(-m.review.pan) },
		},
		{
			id:   actionReviewReload,
			keys: func(k config.Keybindings) []string { return k.Review.Reload },
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.reloadReview() },
		},
		{
			id:   actionReviewEdit,
			keys: func(k config.Keybindings) []string { return k.Review.EditFile },
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.editReviewFile() },
		},
		{
			id:   actionReviewResolve,
			keys: func(k config.Keybindings) []string { return k.Review.Resolve },
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.resolveReviewThread() },
		},
		{
			id:   actionReviewViewed,
			keys: func(k config.Keybindings) []string { return k.Review.ToggleViewed },
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.toggleReviewViewed() },
		},
		{
			id:   actionReviewFocusThread,
			keys: func(k config.Keybindings) []string { return k.Review.FocusThread },
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.focusReviewThread(), nil },
		},
		// The two keys that write are taken whether or not anybody is there to read, so that a
		// habit press is answered with the reason rather than by nothing happening at all.
		{
			id:   actionReviewCompose,
			keys: func(k config.Keybindings) []string { return k.Review.Compose },
			run:  orRefuseWrite(func(m model) (tea.Model, tea.Cmd) { return m.composeReview() }),
		},
		{
			id:   actionReviewSend,
			keys: func(k config.Keybindings) []string { return k.Form.Submit },
			run:  orRefuseWrite(func(m model) (tea.Model, tea.Cmd) { return m.openReviewRelease() }),
		},
	}
}

// orRefuseWrite turns an arm that puts something in front of the agent into one that says why it
// did not, when the agent it would reach is no longer there.
func orRefuseWrite(fn func(model) (tea.Model, tea.Cmd)) func(model, string) (tea.Model, tea.Cmd) {
	return func(m model, _ string) (tea.Model, tea.Cmd) {
		if _, dead := m.reviewIsReadOnly(); dead {
			return m.refuseReviewWrite()
		}

		return fn(m)
	}
}

func reviewLegend() []legendRow {
	// A dead agent keeps every key that reads and loses every key that sends, so the ones that
	// would only explain their own refusal are not offered.
	writable := func(m model) bool {
		_, dead := m.reviewIsReadOnly()

		return !dead
	}

	return []legendRow{
		{
			of:    []actionID{actionMove},
			desc:  func(model) string { return "move" },
			label: moveLabel,
			strip: true,
		},
		{
			of:   []actionID{actionReviewScroll},
			desc: func(model) string { return "scroll" },
			label: func(k config.Keybindings) string {
				return headLabel(k.Review.ScrollDown) + "/" + headLabel(k.Review.ScrollUp)
			},
		},
		{
			of:   []actionID{actionReviewFile},
			desc: func(model) string { return "next/previous file" },
			label: func(k config.Keybindings) string {
				return headLabel(k.Review.NextFile) + "/" + headLabel(k.Review.PrevFile)
			},
		},
		{
			of:   []actionID{actionReviewThread},
			desc: func(model) string { return "next/previous thread" },
			label: func(k config.Keybindings) string {
				return headLabel(k.Review.NextThread) + "/" + headLabel(k.Review.PrevThread)
			},
		},
		{
			of:   []actionID{actionFilterOpen},
			desc: func(model) string { return "search" },
		},
		{
			of:   []actionID{actionReviewMatch},
			desc: func(model) string { return "next/previous match" },
			label: func(k config.Keybindings) string {
				return headLabel(k.Review.NextMatch) + "/" + headLabel(k.Review.PrevMatch)
			},
		},
		// Panning and its reset share a row, since the reset is part of the gesture and the
		// sidebar has no line to spare on the screens this legend has to fit.
		{
			of:   []actionID{actionReviewPan, actionReviewPanReset},
			desc: func(model) string { return "pan sideways" },
			label: func(k config.Keybindings) string {
				return headLabel(k.Review.PanLeft) + "/" + headLabel(k.Review.PanRight) + "/" + keyLabel(k.Review.PanReset)
			},
		},
		{
			of:    []actionID{actionJump},
			desc:  func(model) string { return "top/bottom" },
			label: jumpLabel,
		},
		{
			of:   []actionID{actionReviewEdit},
			desc: model.reviewEditorDesc,
		},
		{
			of:   []actionID{actionReviewReload},
			desc: model.reviewReloadDesc,
		},
		{
			of:   []actionID{actionReviewResolve},
			desc: model.reviewResolveDesc,
		},
		{
			of:   []actionID{actionReviewViewed},
			desc: model.reviewViewedDesc,
		},
		{
			of:   []actionID{actionReviewFocusThread},
			desc: model.reviewThreadDesc,
		},
		{
			of:    []actionID{actionReviewCompose},
			desc:  model.reviewComposeDesc,
			show:  writable,
			strip: true,
		},
		{
			of:   []actionID{actionReviewSend},
			desc: func(model) string { return "send the review" },
			show: writable,
		},
		{
			of:   []actionID{actionReviewBack},
			desc: func(model) string { return "back to the agents" },
		},
	}
}

func (m model) moveReview(pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Nav.Down) {
		return m.moveReviewCursor(m.review.view.Move(m.review.cursor, 1))
	}

	return m.moveReviewCursor(m.review.view.Move(m.review.cursor, -1))
}

func (m model) jumpReview(pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Nav.Bottom) {
		return m.moveReviewCursor(m.review.view.GetLastRow())
	}

	return m.moveReviewCursor(m.review.view.GetFirstRow())
}

func (m model) scrollReviewBy(pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Review.ScrollUp) {
		return m.scrollReview(-1)
	}

	return m.scrollReview(1)
}

func (m model) stepReviewFile(pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Review.PrevFile) {
		return m.jumpReviewFile(-1)
	}

	return m.jumpReviewFile(1)
}

func (m model) stepReviewThread(pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Review.PrevThread) {
		return m.moveReviewCursor(m.review.view.JumpThread(m.review.cursor, -1))
	}

	return m.moveReviewCursor(m.review.view.JumpThread(m.review.cursor, 1))
}

func (m model) stepReviewMatch(pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Review.PrevMatch) {
		return m.jumpReviewMatch(-1)
	}

	return m.jumpReviewMatch(1)
}

func (m model) panReviewBy(pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Review.PanLeft) {
		return m.panReview(-m.reviewPanStep())
	}

	return m.panReview(m.reviewPanStep())
}

func (m model) openReviewFilter(string) (tea.Model, tea.Cmd) {
	m.review.filter.typing = true
	m.review.filter.setQuery("")
	m.searchReview()

	return m, nil
}
