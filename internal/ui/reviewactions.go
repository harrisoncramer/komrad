package ui

import (
	"slices"

	"komrad/config"

	tea "charm.land/bubbletea/v2"
)

// actions is the keys of whichever of the diff, the file tree and the pull request holds them.
func (r *reviewScreen) actions(m *model) screenActionSpec {
	if r.isPullRequestFocused(m) {
		return r.pullRequestActions()
	}
	if r.isBlockFocused(m) {
		return r.filesActions()
	}

	return r.diffActions()
}

// diffActions is the review screen as one table, walked in precedence order by the dispatch and
// named in reading order by a legend of the same ids.
func (r *reviewScreen) diffActions() screenActionSpec {
	return screenActionSpec{
		table:  r.table(),
		legend: r.legend(),
	}
}

func (r *reviewScreen) table() []screenAction {
	return slices.Concat(r.ownTable(), r.diffKeys().table())
}

// ownTable is the review's keys past the ones every diff pane shares.
func (r *reviewScreen) ownTable() []screenAction {
	return []screenAction{
		{
			id:     actionReviewBack,
			keys:   func(k config.Keybindings) []string { return k.Review.Back },
			direct: directly(editorRelease, func(m *model, _ string) tea.Cmd { return r.backFromDiff(m) }),
		},
		{
			id:   actionQuit,
			keys: func(k config.Keybindings) []string { return k.Review.Quit },
			nav:  func(m *model, _ string) tea.Cmd { return m.hide() },
		},
		{
			id:   actionReviewThread,
			keys: func(k config.Keybindings) []string { return slices.Concat(k.Review.NextThread, k.Review.PrevThread) },
			nav:  r.stepThread,
		},
		{
			id: actionReviewOpenThread,
			keys: func(k config.Keybindings) []string {
				return slices.Concat(k.Review.NextOpenThread, k.Review.PrevOpenThread)
			},
			nav: r.stepOpenThread,
		},
		{
			id:   actionReviewBase,
			keys: func(k config.Keybindings) []string { return k.Review.Base },
			when: func(*model) bool { return r.doesWalk() },
			nav:  func(m *model, _ string) tea.Cmd { return r.stepBase(m) },
		},
		{
			id:   actionReviewBaseForward,
			keys: func(k config.Keybindings) []string { return k.Review.BaseForward },
			when: func(*model) bool { return r.doesWalk() },
			nav:  func(m *model, _ string) tea.Cmd { return r.stepBaseForward(m) },
		},
		{
			id:   actionReviewAnswer,
			keys: func(k config.Keybindings) []string { return k.Review.Answer },
			when: func(*model) bool { return r.doesOfferAnswer() },
			nav:  func(m *model, _ string) tea.Cmd { return r.toggleAnswer(m) },
		},
		{
			id:   actionReviewJump,
			keys: func(k config.Keybindings) []string { return k.Review.Jump },
			when: func(*model) bool { return r.doesOfferJump() },
			nav:  func(m *model, _ string) tea.Cmd { return r.toggleJump(m) },
		},
		{
			id:   actionReviewReload,
			keys: func(k config.Keybindings) []string { return k.Review.Reload },
			nav:  func(m *model, _ string) tea.Cmd { return r.reloadFresh(m) },
		},
		{
			id:   actionReviewEdit,
			keys: func(k config.Keybindings) []string { return k.Review.EditFile },
			// Editing writes to the worktree rather than to the agent, so it is offered whether or
			// not there is anybody there to read a comment.
			direct: directly(notInMenu, func(m *model, _ string) tea.Cmd { return r.pressEdit(m) }),
		},
		{
			id:     actionReviewResolve,
			keys:   func(k config.Keybindings) []string { return k.Review.Resolve },
			direct: directly(notInMenu, func(m *model, _ string) tea.Cmd { return r.resolveThread(m) }),
		},
		{
			id:     actionReviewAccept,
			keys:   func(k config.Keybindings) []string { return k.Review.Accept },
			when:   func(*model) bool { return r.doesOfferAccept() },
			direct: directly(notInMenu, r.orRefuseWrite(func(m *model) tea.Cmd { return r.acceptForge(m) })),
		},
		{
			id:     actionReviewForgeReply,
			keys:   func(k config.Keybindings) []string { return k.Review.ForgeReply },
			when:   func(*model) bool { return r.doesOfferForgeReply() },
			direct: directly(notInMenu, r.orRefuseComment(func(m *model) tea.Cmd { return r.composeForge(m) })),
		},
		{
			id:     actionReviewForgePost,
			keys:   func(k config.Keybindings) []string { return k.Review.ForgePost },
			when:   func(*model) bool { return r.doesOfferForgeRetry() },
			direct: directly(notInMenu, r.orRefuseComment(func(m *model) tea.Cmd { return r.retryForgeReply(m) })),
		},
		{
			id:     actionReviewForgeDiscard,
			keys:   func(k config.Keybindings) []string { return k.Review.ForgeDiscard },
			when:   func(*model) bool { return r.doesOfferForgeRetry() },
			direct: directly(notInMenu, r.orRefuseComment(func(m *model) tea.Cmd { return r.discardForgeReply(m) })),
		},
		{
			id:     actionReviewViewed,
			keys:   func(k config.Keybindings) []string { return k.Review.ToggleViewed },
			direct: directly(viewedIsLocal, func(m *model, _ string) tea.Cmd { return r.toggleViewed(m) }),
		},
		// The two keys that write are taken whether or not anybody is there to read.
		{
			id:     actionReviewCompose,
			keys:   func(k config.Keybindings) []string { return k.Review.Compose },
			direct: directly(composerSurface, r.orRefuseComment(func(m *model) tea.Cmd { return r.compose(m) })),
		},
		{
			id: actionReviewSend,
			// Comments wait in a batch until sent, so this is offered while reading as well as
			// while writing.
			keys:   func(k config.Keybindings) []string { return k.Review.Send },
			direct: directly(composerSurface, r.orRefuseWrite(func(m *model) tea.Cmd { return r.openRelease(m) })),
		},
	}
}

// orRefuseWrite turns an arm that puts something in front of the agent into one that says why it
// did not, when the agent it would reach is no longer there.
func (r *reviewScreen) orRefuseWrite(fn func(*model) tea.Cmd) func(*model, string) tea.Cmd {
	return func(m *model, _ string) tea.Cmd {
		if r.isArchived(m) {
			r.refuseWrite(m)

			return nil
		}

		if said, history := r.isHistory(); history {
			r.refuseHistory(m, said)

			return nil
		}

		return fn(m)
	}
}

func (r *reviewScreen) orRefuseComment(fn func(*model) tea.Cmd) func(*model, string) tea.Cmd {
	return func(m *model, pressed string) tea.Cmd {
		if _, replying := r.diff.threadAtCursor(); !replying {
			return r.orRefuseWrite(fn)(m, pressed)
		}

		if r.isArchived(m) {
			r.refuseWrite(m)

			return nil
		}

		return fn(m)
	}
}

func (r *reviewScreen) legend() []screenLegendRow {
	// An archived agent keeps every key that reads and loses every key that sends, so the ones that
	// would only explain their own refusal are not offered.
	reachable := func(m *model) bool {
		return !r.isArchived(m)
	}

	// An older round holds no line a comment could ask to be changed, so the keys that write are
	// not offered on one.
	offered := func(m *model) bool {
		_, history := r.isHistory()

		return reachable(m) && !history
	}

	commentable := func(m *model) bool {
		_, replying := r.diff.threadAtCursor()

		return offered(m) || (reachable(m) && replying)
	}

	return []screenLegendRow{
		navMoveRow("move"),
		navPageRow(),
		diffFileRow(),
		{
			of:   []actionID{actionReviewThread, actionReviewOpenThread},
			desc: func(*model) string { return r.diff.threadStepDesc() },
			label: func(k config.Keybindings) string {
				return headLabel(k.Review.NextThread) + "/" + headLabel(k.Review.PrevThread) +
					"/" + headLabel(k.Review.NextOpenThread) + "/" + headLabel(k.Review.PrevOpenThread)
			},
		},
		diffSearchRow(),
		diffMatchRow(),
		diffPanRow(),
		diffWrapRow(),
		navJumpRow(),
		{
			of:   []actionID{actionReviewEdit},
			desc: func(*model) string { return r.hold.editorDesc() },
			// The one thing standing in the way of editing is an earlier round, whose line numbers
			// name a file the worktree no longer holds.
			show: func(*model) bool {
				_, history := r.isHistory()

				return !history
			},
			strip: true,
		},
		{
			of:   []actionID{actionReviewBase},
			desc: func(*model) string { return "an earlier chapter" },
			show: r.doesFitBaseLegend,
		},
		{
			of:   []actionID{actionReviewAnswer},
			desc: func(*model) string { return r.diff.answerDesc() },
			show: func(*model) bool { return r.doesOfferAnswer() },
		},
		{
			of:   []actionID{actionReviewJump},
			desc: func(*model) string { return r.diff.jumpDesc() },
			show: func(*model) bool { return r.doesOfferJump() },
		},
		{
			of:   []actionID{actionReviewReload},
			desc: func(m *model) string { return r.reloadDesc(m) },
		},
		{
			of:   []actionID{actionReviewResolve},
			desc: func(*model) string { return r.diff.resolveDesc() },
		},
		{
			of:   []actionID{actionReviewAccept},
			desc: func(*model) string { return "accept the forge comment" },
			show: func(*model) bool { return r.doesOfferAccept() },
		},
		{
			of:   []actionID{actionReviewForgeReply},
			desc: func(m *model) string { return "reply on " + r.forgeName(m) },
			show: func(m *model) bool { return reachable(m) && r.doesOfferForgeReply() },
		},
		{
			of:   []actionID{actionReviewForgePost},
			desc: func(*model) string { return "retry the reply" },
			show: func(m *model) bool { return reachable(m) && r.doesOfferForgeRetry() },
		},
		{
			of:   []actionID{actionReviewForgeDiscard},
			desc: func(*model) string { return "discard the reply" },
			show: func(m *model) bool { return reachable(m) && r.doesOfferForgeRetry() },
		},
		{
			of:   []actionID{actionReviewViewed, actionDiffExpand},
			desc: func(*model) string { return r.diff.fileDesc() },
		},
		{
			of:   []actionID{actionReviewBaseForward},
			desc: func(*model) string { return "a later chapter" },
			show: r.doesFitBaseLegend,
		},
		{
			of:    []actionID{actionReviewCompose},
			desc:  func(*model) string { return r.composeDesc() },
			show:  commentable,
			strip: true,
		},
		{
			of:   []actionID{actionReviewSend},
			desc: func(*model) string { return r.threads.sendDesc() },
			show: offered,
		},
		{
			of:   []actionID{actionReviewBack},
			desc: func(m *model) string { return r.diffBackDesc(m) },
		},
	}
}

// stepOpenThread walks the comments nobody has settled.
func (r *reviewScreen) stepOpenThread(m *model, pressed string) tea.Cmd {
	if isBound(pressed, m.keys.Review.PrevOpenThread) {
		return r.jumpOpenThread(m, -1)
	}

	return r.jumpOpenThread(m, 1)
}

func (r *reviewScreen) stepThread(m *model, pressed string) tea.Cmd {
	if isBound(pressed, m.keys.Review.PrevThread) {
		return r.moveCursor(m, r.diff.View().JumpThread(r.diff.Cursor(), -1))
	}

	return r.moveCursor(m, r.diff.View().JumpThread(r.diff.Cursor(), 1))
}

// filesActions is the keys on the file tree, which walk the diff a file at a time.
func (r *reviewScreen) filesActions() screenActionSpec {
	standing := func(*model) bool { return r.filesFilter.isStanding() }

	return screenActionSpec{
		table: append(navActions(filesNav{r}, navSteps{page: navPageStep, scroll: 1}), []screenAction{
			{
				id:   actionDiffFile,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Diff.NextFile, k.Diff.PrevFile) },
				nav: func(m *model, pressed string) tea.Cmd {
					return r.stepFile(m, directionOf(pressed, m.keys.Diff.PrevFile))
				},
			},
			{
				id:   actionFilterOpen,
				keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
				nav: func(*model, string) tea.Cmd {
					r.openFilesFilter()

					return nil
				},
			},
			{
				id:   actionFilterMatch,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Filter.NextMatch, k.Filter.PrevMatch) },
				when: standing,
				nav: func(m *model, pressed string) tea.Cmd {
					return r.jumpFilesMatch(m, directionOf(pressed, m.keys.Filter.PrevMatch))
				},
			},
			{
				id:   actionFilterClear,
				keys: func(k config.Keybindings) []string { return k.Review.Back },
				when: standing,
				nav: func(*model, string) tea.Cmd {
					r.filesFilter.abandon()

					return nil
				},
			},
			{
				id:     actionReviewBack,
				keys:   func(k config.Keybindings) []string { return k.Review.Back },
				direct: directly(editorRelease, func(m *model, _ string) tea.Cmd { return r.back(m) }),
			},
			{
				id:   actionQuit,
				keys: func(k config.Keybindings) []string { return k.Review.Quit },
				nav:  func(m *model, _ string) tea.Cmd { return m.hide() },
			},
			{
				id:     actionReviewViewed,
				keys:   func(k config.Keybindings) []string { return k.Review.ToggleViewed },
				direct: directly(viewedIsLocal, func(m *model, _ string) tea.Cmd { return r.toggleViewed(m) }),
			},
		}...),
		legend: []screenLegendRow{
			navMoveRow("move between files"),
			navPageRow(),
			navJumpRow(),
			diffFileRow(),
			{
				of:   []actionID{actionFilterOpen},
				desc: func(*model) string { return r.filesFilter.openDesc() },
			},
			{
				of:   []actionID{actionFilterMatch},
				desc: func(*model) string { return "next/prev match" },
			},
			{
				of:   []actionID{actionFilterClear},
				desc: func(*model) string { return r.filesFilter.clearDesc() },
			},
			{
				of:   []actionID{actionReviewViewed},
				desc: func(*model) string { return r.diff.viewedDesc() },
				show: func(*model) bool { return !r.diff.spec.isNarrowed() },
			},
			{
				of:    []actionID{actionReviewBack},
				desc:  func(*model) string { return r.backDesc() },
				show:  func(*model) bool { return !r.filesFilter.isStanding() },
				strip: true,
			},
		},
	}
}

// The review's own reasons for an arm that acts straight off its key.
const (
	viewedIsLocal   = "the fold is local and immediate, and holding a reading key on its echo would slow it"
	editorRelease   = "leaving the editor releases the agent whichever key leaves it"
	composerSurface = "the composer and the release panel are the review's own surface with their own submit"
)
