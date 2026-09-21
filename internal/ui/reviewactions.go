package ui

import (
	"weave/config"

	tea "github.com/charmbracelet/bubbletea"
)

// reviewActions is the review screen as one table, walked in precedence order by the dispatch
// and named in reading order by a legend of the same ids.
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
			// Writing is a mode inside the screen, so the back key gives up the mode before it
			// gives up the screen.
			run: func(m model, _ string) (tea.Model, tea.Cmd) {
				if m.review.mode == modeReviewing {
					return m.leaveReviewWriting()
				}

				return m.leaveReview()
			},
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
			id:   actionNavPage,
			keys: func(k config.Keybindings) []string { return concatKeys(k.Nav.PageUp, k.Nav.PageDown) },
			run:  model.pageReview,
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
			id: actionReviewOpenThread,
			keys: func(k config.Keybindings) []string {
				return concatKeys(k.Review.NextOpenThread, k.Review.PrevOpenThread)
			},
			run: model.stepOpenThread,
		},
		{
			id:   actionFilterOpen,
			keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
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
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.panReview(-m.review.diff.pan) },
		},
		{
			id:   actionReviewBase,
			keys: func(k config.Keybindings) []string { return k.Review.Base },
			when: model.doesReviewWalk,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.stepReviewBase() },
		},
		{
			id:   actionReviewBaseForward,
			keys: func(k config.Keybindings) []string { return k.Review.BaseForward },
			when: model.doesReviewWalk,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.stepReviewBaseForward() },
		},
		{
			id:   actionReviewReload,
			keys: func(k config.Keybindings) []string { return k.Review.Reload },
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.reloadReview() },
		},
		{
			id:   actionReviewEdit,
			keys: func(k config.Keybindings) []string { return k.Review.EditFile },
			// Editing writes to the worktree rather than to the agent, so it is offered whether or
			// not there is anybody there to read a comment.
			run: func(m model, _ string) (tea.Model, tea.Cmd) { return m.pressReviewEdit() },
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
			id:   actionReviewExpand,
			keys: func(k config.Keybindings) []string { return k.Review.Expand },
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.toggleReviewExpanded() },
		},
		{
			id:   actionReviewFocusThread,
			keys: func(k config.Keybindings) []string { return k.Review.FocusThread },
			run: func(m model, _ string) (tea.Model, tea.Cmd) {
				m.review.focusThread()

				return m, nil
			},
		},
		// The two keys that write are taken whether or not anybody is there to read.
		{
			id:   actionReviewCompose,
			keys: func(k config.Keybindings) []string { return k.Review.Compose },
			run:  orRefuseWrite(model.pressReviewCompose),
		},
		{
			id: actionReviewSend,
			// A verdict with no comments is how a change is approved, so this is offered while
			// reading as well as while writing.
			keys: func(config.Keybindings) []string { return formKeys().Submit },
			run:  orRefuseWrite(model.pressReviewSend),
		},
	}
}

// orRefuseWrite turns an arm that puts something in front of the agent into one that says why it
// did not, when the agent it would reach is no longer there.
func orRefuseWrite(fn func(model) (tea.Model, tea.Cmd)) func(model, string) (tea.Model, tea.Cmd) {
	return func(m model, _ string) (tea.Model, tea.Cmd) {
		if _, dead := m.isReviewReadOnly(); dead {
			return m.refuseReviewWrite()
		}

		if said, history := m.review.isHistory(); history {
			return m.refuseReviewHistory(said)
		}

		return fn(m)
	}
}

// pressReviewCompose is the comment key, which refuses a new thread while a review is owed an
// answer.
func (m model) pressReviewCompose() (tea.Model, tea.Cmd) {
	if _, replying := m.review.diff.threadAtCursor(); m.review.threads.isOwedAnAnswer() && !replying {
		return m.refuseReviewAwaiting()
	}

	return m.composeReview()
}

// pressReviewSend is the send key, which while a review is owed an answer needs replies written
// to send, a verdict on its own being the second review the wait holds off.
func (m model) pressReviewSend() (tea.Model, tea.Cmd) {
	if m.review.threads.isOwedAnAnswer() && !m.review.threads.holdsUnsent() {
		return m.refuseReviewAwaiting()
	}

	return m.openReviewRelease()
}

func reviewLegend() []legendRow {
	// A dead agent keeps every key that reads and loses every key that sends, so the ones that
	// would only explain their own refusal are not offered.
	reachable := func(m model) bool {
		_, dead := m.isReviewReadOnly()

		return !dead
	}

	// An older round holds no line a comment could ask to be changed, so the keys that write are
	// not offered on one.
	offered := func(m model) bool {
		_, history := m.review.isHistory()

		return reachable(m) && !history
	}

	return []legendRow{
		{
			of:    []actionID{actionMove},
			desc:  func(model) string { return "move" },
			label: moveLabel,
			strip: true,
		},
		{
			of:    []actionID{actionNavPage},
			desc:  func(model) string { return "page" },
			label: navPageLabel,
		},
		{
			of:   []actionID{actionReviewFile},
			desc: func(model) string { return "next/previous file" },
			label: func(k config.Keybindings) string {
				return headLabel(k.Review.NextFile) + "/" + headLabel(k.Review.PrevFile)
			},
		},
		{
			of:   []actionID{actionReviewThread, actionReviewOpenThread},
			desc: func(m model) string { return m.review.diff.threadStepDesc() },
			label: func(k config.Keybindings) string {
				return headLabel(k.Review.NextThread) + "/" + headLabel(k.Review.PrevThread) +
					"/" + headLabel(k.Review.NextOpenThread) + "/" + headLabel(k.Review.PrevOpenThread)
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
		// Panning and its reset share a row.
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
			// The one thing standing in the way of editing is an earlier round, whose line numbers
			// name a file the worktree no longer holds.
			show: func(m model) bool {
				_, history := m.review.isHistory()

				return !history
			},
			strip: true,
		},
		{
			of:   []actionID{actionReviewBase},
			desc: func(model) string { return "an earlier round" },
			show: model.doesReviewFitBaseLegend,
		},
		{
			of:   []actionID{actionReviewReload},
			desc: func(m model) string { return m.review.diff.reloadDesc() },
		},
		{
			of:   []actionID{actionReviewResolve},
			desc: func(m model) string { return m.review.diff.resolveDesc() },
		},
		{
			of:   []actionID{actionReviewViewed, actionReviewExpand},
			desc: func(m model) string { return m.review.diff.fileDesc() },
		},
		{
			of:   []actionID{actionReviewFocusThread},
			desc: model.reviewThreadDesc,
		},
		{
			of:   []actionID{actionReviewBaseForward},
			desc: func(model) string { return "a later round" },
			show: model.doesReviewFitBaseLegend,
		},
		{
			of:   []actionID{actionReviewCompose},
			desc: func(m model) string { return m.review.composeDesc() },
			// The compose key is the way into the mode that writes, so reading offers it too.
			show:  offered,
			strip: true,
		},
		{
			of:   []actionID{actionReviewSend},
			desc: func(m model) string { return m.review.threads.sendDesc() },
			show: offered,
		},
		{
			of:   []actionID{actionReviewBack},
			desc: model.reviewBackDesc,
		},
	}
}

func (m model) moveReview(pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Nav.Down) {
		return m.moveReviewCursor(m.review.diff.view.Move(m.review.diff.cursor, 1))
	}

	return m.moveReviewCursor(m.review.diff.view.Move(m.review.diff.cursor, -1))
}

func (m model) jumpReview(pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Nav.Bottom) {
		return m.moveReviewCursor(m.review.diff.view.GetLastRow())
	}

	return m.moveReviewCursor(m.review.diff.view.GetFirstRow())
}

func (m model) pageReview(pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Nav.PageUp) {
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

// stepOpenThread walks the comments nobody has settled.
func (m model) stepOpenThread(pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Review.PrevOpenThread) {
		return m.jumpOpenThread(-1)
	}

	return m.jumpOpenThread(1)
}

func (m model) stepReviewThread(pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Review.PrevThread) {
		return m.moveReviewCursor(m.review.diff.view.JumpThread(m.review.diff.cursor, -1))
	}

	return m.moveReviewCursor(m.review.diff.view.JumpThread(m.review.diff.cursor, 1))
}

func (m model) stepReviewMatch(pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Review.PrevMatch) {
		return m.jumpReviewMatch(-1)
	}

	return m.jumpReviewMatch(1)
}

func (m model) panReviewBy(pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Review.PanLeft) {
		return m.panReview(-m.getReviewPanStep())
	}

	return m.panReview(m.getReviewPanStep())
}

func (m model) openReviewFilter(string) (tea.Model, tea.Cmd) {
	m.review.diff.filter.typing = true
	m.review.diff.filter.setQuery("")
	m.searchReview()

	return m, nil
}
