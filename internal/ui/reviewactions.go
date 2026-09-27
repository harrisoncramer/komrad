package ui

import (
	"slices"

	"komrad/config"

	tea "charm.land/bubbletea/v2"
)

// actions is the review screen as one table, walked in precedence order by the dispatch and
// named in reading order by a legend of the same ids.
func (r *reviewScreen) actions() screenActionSpec {
	return screenActionSpec{
		table:  r.table(),
		legend: r.legend(),
	}
}

func (r *reviewScreen) table() []screenAction {
	return []screenAction{
		{
			id:   actionReviewBack,
			keys: func(k config.Keybindings) []string { return k.Review.Back },
			run:  func(h host, s panelShared, _ string) tea.Cmd { return r.leave(h, s) },
		},
		{
			id:   actionQuit,
			keys: func(k config.Keybindings) []string { return k.Review.Quit },
			run:  func(h host, _ panelShared, _ string) tea.Cmd { return h.hide() },
		},
		{
			id:   actionMove,
			keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.Up, k.Nav.Down) },
			run:  r.movePressed,
		},
		{
			id:   actionJump,
			keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.Top, k.Nav.Bottom) },
			run:  r.jumpPressed,
		},
		{
			id:   actionNavPage,
			keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.PageUp, k.Nav.PageDown) },
			run:  r.pagePressed,
		},
		{
			id:   actionColumnScroll,
			keys: func(k config.Keybindings) []string { return slices.Concat(k.Column.ScrollDown, k.Column.ScrollUp) },
			run:  r.wheelPressed,
		},
		{
			id:   actionReviewFile,
			keys: func(k config.Keybindings) []string { return slices.Concat(k.Review.NextFile, k.Review.PrevFile) },
			run:  r.stepFile,
		},
		{
			id:   actionReviewThread,
			keys: func(k config.Keybindings) []string { return slices.Concat(k.Review.NextThread, k.Review.PrevThread) },
			run:  r.stepThread,
		},
		{
			id: actionReviewOpenThread,
			keys: func(k config.Keybindings) []string {
				return slices.Concat(k.Review.NextOpenThread, k.Review.PrevOpenThread)
			},
			run: r.stepOpenThread,
		},
		{
			id:   actionFilterOpen,
			keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
			run:  r.openFilter,
		},
		{
			id:   actionReviewMatch,
			keys: func(k config.Keybindings) []string { return slices.Concat(k.Review.NextMatch, k.Review.PrevMatch) },
			run:  r.stepMatch,
		},
		{
			id:   actionReviewPan,
			keys: func(k config.Keybindings) []string { return slices.Concat(k.Review.PanLeft, k.Review.PanRight) },
			run:  r.panBy,
		},
		{
			id:   actionReviewPanReset,
			keys: func(k config.Keybindings) []string { return k.Review.PanReset },
			run: func(host, panelShared, string) tea.Cmd {
				r.diff.panBy(-r.diff.pan)

				return nil
			},
		},
		{
			id:   actionReviewBase,
			keys: func(k config.Keybindings) []string { return k.Review.Base },
			when: func(panelShared) bool { return r.doesWalk() },
			run:  func(_ host, s panelShared, _ string) tea.Cmd { return r.stepBase(s) },
		},
		{
			id:   actionReviewBaseForward,
			keys: func(k config.Keybindings) []string { return k.Review.BaseForward },
			when: func(panelShared) bool { return r.doesWalk() },
			run:  func(_ host, s panelShared, _ string) tea.Cmd { return r.stepBaseForward(s) },
		},
		{
			id:   actionReviewAnswer,
			keys: func(k config.Keybindings) []string { return k.Review.Answer },
			when: func(panelShared) bool { return r.doesOfferAnswer() },
			run:  func(_ host, s panelShared, _ string) tea.Cmd { return r.toggleAnswer(s) },
		},
		{
			id:   actionReviewReload,
			keys: func(k config.Keybindings) []string { return k.Review.Reload },
			run:  func(_ host, s panelShared, _ string) tea.Cmd { return r.reloadFresh(s) },
		},
		{
			id:   actionReviewEdit,
			keys: func(k config.Keybindings) []string { return k.Review.EditFile },
			// Editing writes to the worktree rather than to the agent, so it is offered whether or
			// not there is anybody there to read a comment.
			run: func(h host, s panelShared, _ string) tea.Cmd { return r.pressEdit(h, s) },
		},
		{
			id:   actionReviewResolve,
			keys: func(k config.Keybindings) []string { return k.Review.Resolve },
			run:  func(_ host, s panelShared, _ string) tea.Cmd { return r.resolveThread(s) },
		},
		{
			id:   actionReviewViewed,
			keys: func(k config.Keybindings) []string { return k.Review.ToggleViewed },
			run:  func(h host, s panelShared, _ string) tea.Cmd { return r.toggleViewed(h, s) },
		},
		{
			id:   actionReviewExpand,
			keys: func(k config.Keybindings) []string { return k.Review.Expand },
			run: func(_ host, s panelShared, _ string) tea.Cmd {
				r.toggleExpanded(s)

				return nil
			},
		},
		// The two keys that write are taken whether or not anybody is there to read.
		{
			id:   actionReviewCompose,
			keys: func(k config.Keybindings) []string { return k.Review.Compose },
			run:  r.orRefuseWrite(func(_ host, s panelShared) tea.Cmd { return r.compose(s) }),
		},
		{
			id: actionReviewSend,
			// Comments wait in a batch until sent, so this is offered while reading as well as
			// while writing.
			keys: func(config.Keybindings) []string { return formKeys().Submit },
			run:  r.orRefuseWrite(func(_ host, s panelShared) tea.Cmd { return r.openRelease(s) }),
		},
	}
}

// orRefuseWrite turns an arm that puts something in front of the agent into one that says why it
// did not, when the agent it would reach is no longer there.
func (r *reviewScreen) orRefuseWrite(fn func(host, panelShared) tea.Cmd) func(host, panelShared, string) tea.Cmd {
	return func(h host, s panelShared, _ string) tea.Cmd {
		if _, dead := r.isReadOnly(s); dead {
			r.refuseWrite(s)

			return nil
		}

		if said, history := r.isHistory(); history {
			r.refuseHistory(h, s, said)

			return nil
		}

		return fn(h, s)
	}
}

func (r *reviewScreen) legend() []screenLegendRow {
	// A dead agent keeps every key that reads and loses every key that sends, so the ones that
	// would only explain their own refusal are not offered.
	reachable := func(s panelShared) bool {
		_, dead := r.isReadOnly(s)

		return !dead
	}

	// An older round holds no line a comment could ask to be changed, so the keys that write are
	// not offered on one.
	offered := func(s panelShared) bool {
		_, history := r.isHistory()

		return reachable(s) && !history
	}

	return []screenLegendRow{
		{
			of:    []actionID{actionMove},
			desc:  func(panelShared) string { return "move" },
			label: moveLabel,
			strip: true,
		},
		{
			of:    []actionID{actionNavPage},
			desc:  func(panelShared) string { return "page" },
			label: navPageLabel,
		},
		{
			of:   []actionID{actionReviewFile},
			desc: func(panelShared) string { return "next/previous file" },
			label: func(k config.Keybindings) string {
				return headLabel(k.Review.NextFile) + "/" + headLabel(k.Review.PrevFile)
			},
		},
		{
			of:   []actionID{actionReviewThread, actionReviewOpenThread},
			desc: func(panelShared) string { return r.diff.threadStepDesc() },
			label: func(k config.Keybindings) string {
				return headLabel(k.Review.NextThread) + "/" + headLabel(k.Review.PrevThread) +
					"/" + headLabel(k.Review.NextOpenThread) + "/" + headLabel(k.Review.PrevOpenThread)
			},
		},
		{
			of:   []actionID{actionFilterOpen},
			desc: func(panelShared) string { return "search" },
		},
		{
			of:   []actionID{actionReviewMatch},
			desc: func(panelShared) string { return "next/previous match" },
			label: func(k config.Keybindings) string {
				return headLabel(k.Review.NextMatch) + "/" + headLabel(k.Review.PrevMatch)
			},
		},
		// Panning and its reset share a row.
		{
			of:   []actionID{actionReviewPan, actionReviewPanReset},
			desc: func(panelShared) string { return "pan sideways" },
			label: func(k config.Keybindings) string {
				return headLabel(k.Review.PanLeft) + "/" + headLabel(k.Review.PanRight) + "/" + keyLabel(k.Review.PanReset)
			},
		},
		{
			of:    []actionID{actionJump},
			desc:  func(panelShared) string { return "top/bottom" },
			label: jumpLabel,
		},
		{
			of:   []actionID{actionReviewEdit},
			desc: func(panelShared) string { return r.hold.editorDesc() },
			// The one thing standing in the way of editing is an earlier round, whose line numbers
			// name a file the worktree no longer holds.
			show: func(panelShared) bool {
				_, history := r.isHistory()

				return !history
			},
			strip: true,
		},
		{
			of:   []actionID{actionReviewBase},
			desc: func(panelShared) string { return "an earlier round" },
			show: r.doesFitBaseLegend,
		},
		{
			of:   []actionID{actionReviewAnswer},
			desc: func(panelShared) string { return r.diff.answerDesc() },
			show: func(panelShared) bool { return r.doesOfferAnswer() },
		},
		{
			of:   []actionID{actionReviewReload},
			desc: func(s panelShared) string { return r.reloadDesc(s) },
		},
		{
			of:   []actionID{actionReviewResolve},
			desc: func(panelShared) string { return r.diff.resolveDesc() },
		},
		{
			of:   []actionID{actionReviewViewed, actionReviewExpand},
			desc: func(panelShared) string { return r.diff.fileDesc() },
		},
		{
			of:   []actionID{actionReviewBaseForward},
			desc: func(panelShared) string { return "a later round" },
			show: r.doesFitBaseLegend,
		},
		{
			of:    []actionID{actionReviewCompose},
			desc:  func(panelShared) string { return r.composeDesc() },
			show:  offered,
			strip: true,
		},
		{
			of:   []actionID{actionReviewSend},
			desc: func(panelShared) string { return r.threads.sendDesc() },
			show: offered,
		},
		{
			of:   []actionID{actionReviewBack},
			desc: func(panelShared) string { return r.backDesc() },
		},
	}
}

func (r *reviewScreen) movePressed(_ host, s panelShared, pressed string) tea.Cmd {
	if isBound(pressed, s.keys.Nav.Down) {
		return r.moveCursor(s, r.diff.view.Move(r.diff.cursor, 1))
	}

	return r.moveCursor(s, r.diff.view.Move(r.diff.cursor, -1))
}

func (r *reviewScreen) jumpPressed(_ host, s panelShared, pressed string) tea.Cmd {
	if isBound(pressed, s.keys.Nav.Bottom) {
		return r.moveCursor(s, r.diff.view.GetLastRow())
	}

	return r.moveCursor(s, r.diff.view.GetFirstRow())
}

func (r *reviewScreen) pagePressed(_ host, s panelShared, pressed string) tea.Cmd {
	if isBound(pressed, s.keys.Nav.PageUp) {
		return r.scroll(s, -1)
	}

	return r.scroll(s, 1)
}

func (r *reviewScreen) stepFile(_ host, s panelShared, pressed string) tea.Cmd {
	if isBound(pressed, s.keys.Review.PrevFile) {
		return r.jumpFile(s, -1)
	}

	return r.jumpFile(s, 1)
}

// stepOpenThread walks the comments nobody has settled.
func (r *reviewScreen) stepOpenThread(_ host, s panelShared, pressed string) tea.Cmd {
	if isBound(pressed, s.keys.Review.PrevOpenThread) {
		return r.jumpOpenThread(s, -1)
	}

	return r.jumpOpenThread(s, 1)
}

func (r *reviewScreen) stepThread(_ host, s panelShared, pressed string) tea.Cmd {
	if isBound(pressed, s.keys.Review.PrevThread) {
		return r.moveCursor(s, r.diff.view.JumpThread(r.diff.cursor, -1))
	}

	return r.moveCursor(s, r.diff.view.JumpThread(r.diff.cursor, 1))
}

func (r *reviewScreen) stepMatch(_ host, s panelShared, pressed string) tea.Cmd {
	if isBound(pressed, s.keys.Review.PrevMatch) {
		return r.jumpMatch(s, -1)
	}

	return r.jumpMatch(s, 1)
}

func (r *reviewScreen) panBy(_ host, s panelShared, pressed string) tea.Cmd {
	if isBound(pressed, s.keys.Review.PanLeft) {
		r.diff.panBy(-panStep(s))

		return nil
	}

	r.diff.panBy(panStep(s))

	return nil
}

func (r *reviewScreen) openFilter(_ host, s panelShared, _ string) tea.Cmd {
	r.diff.filter.typing = true
	r.diff.filter.setQuery("")
	r.search(s)

	return nil
}
