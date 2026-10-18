package ui

import (
	"fmt"
	"slices"

	"komrad/config"

	tea "charm.land/bubbletea/v2"
)

// actions is the keys of whichever of the diff and the sidebar holds them.
func (r *reviewScreen) actions(m *model) screenActionSpec {
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
			keys:   fixedBack,
			direct: directly(editorRelease, func(m *model, _ string) tea.Cmd { return r.backFromDiff(m) }),
		},
		{
			id:   actionQuit,
			keys: fixedQuit,
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
	}
}

// orRefuseWrite runs fn, which puts something in front of the agent, or says why it did not, when
// the agent it would reach is no longer there.
func (r *reviewScreen) orRefuseWrite(m *model, fn func(*model) tea.Cmd) tea.Cmd {
	if r.isArchived(m) {
		r.refuseWrite(m)

		return nil
	}

	return fn(m)
}

func (r *reviewScreen) legend() []screenLegendRow {
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
		navJumpRow(),
		{
			of:   []actionID{actionReviewBase},
			desc: func(*model) string { return "an earlier review" },
			show: r.doesFitBaseLegend,
		},
		{
			of:   []actionID{actionReviewBaseForward},
			desc: func(*model) string { return "a later review" },
			show: r.doesFitBaseLegend,
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

// filesActions is the keys on the file tree, which walk it a row at a time with the diff following.
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
				id: actionReviewFolder,
				keys: func(k config.Keybindings) []string {
					return slices.Concat(fixedOpen(k), k.Review.ToggleFolder)
				},
				nav: func(m *model, _ string) tea.Cmd {
					if cmd, chosen := r.chooseSide(m); chosen {
						return cmd
					}

					r.toggleFolder()

					return nil
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
				keys: fixedBack,
				when: standing,
				nav: func(*model, string) tea.Cmd {
					r.filesFilter.abandon()

					return nil
				},
			},
			{
				id:     actionReviewBack,
				keys:   fixedBack,
				direct: directly(editorRelease, func(m *model, _ string) tea.Cmd { return r.back(m) }),
			},
			{
				id:   actionQuit,
				keys: fixedQuit,
				nav:  func(m *model, _ string) tea.Cmd { return m.hide() },
			},
		}...),
		legend: []screenLegendRow{
			navMoveRow("move"),
			navPageRow(),
			navJumpRow(),
			diffFileRow(),
			{
				of:   []actionID{actionReviewFolder},
				desc: func(*model) string { return r.chooseDesc() },
				show: func(*model) bool { return r.doesChoose() },
			},
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
	editorRelease = "leaving the editor releases the agent whichever key leaves it"
)

// buildItemMenu builds the band on the thread under the diff's cursor.
func (r *reviewScreen) buildItemMenu(m *model) (submenu, bool) {
	if r.isBlockFocused(m) && r.side != nil {
		return r.sideMenu(m)
	}

	if !r.isDiffFocused(m) {
		return submenu{}, false
	}

	_, ok := r.diff.threadAtCursor()
	if !ok {
		return submenu{}, false
	}

	reachable := !r.isArchived(m)
	entries := []submenuEntry{}

	if reachable {
		entries = append(entries, r.composeEntry(m, ""))
	}

	if reachable && r.doesOfferForgeReply() {
		entries = append(entries, openEntry("reply on "+r.forgeName(m), m.keys.Review.ForgeReply, func(m *model) tea.Cmd {
			return m.review.orRefuseWrite(m, m.review.composeForge)
		}))
	}

	entries = append(entries, openEntry(r.diff.resolveDesc(), m.keys.Review.Resolve, func(m *model) tea.Cmd { return m.review.resolveThread(m) }))

	if r.doesOfferAccept() {
		accept := openEntry("accept the forge comment", m.keys.Review.Accept, func(m *model) tea.Cmd {
			return m.review.orRefuseWrite(m, m.review.acceptForge)
		})
		entries = append(entries, drawnOff(accept, r.diff.newestSaid()))
	}

	if reachable && r.doesOfferForgeRetry() {
		entries = append(entries,
			openEntry("retry the reply", m.keys.Review.ForgePost, func(m *model) tea.Cmd {
				return m.review.orRefuseWrite(m, m.review.retryForgeReply)
			}),
			openEntry("discard the reply", m.keys.Review.ForgeDiscard, func(m *model) tea.Cmd {
				return m.review.orRefuseWrite(m, m.review.discardForgeReply)
			}),
		)
	}

	if r.doesOfferAnswer() {
		entries = append(entries, r.answerEntry(m))
	}

	entries = append(entries, drawnOff(r.jumpEntry(m), r.diff.jumpOffSaid()))

	return submenu{
		kind:    formMenu,
		title:   "thread",
		entries: entries,
	}, true
}

// buildScreenMenu builds the review's own band, for the file in the editor, how the diff is rendered
// and the review waiting to be sent.
func (r *reviewScreen) buildScreenMenu(m *model) (submenu, bool) {
	reachable := !r.isArchived(m)
	_, onThread := r.diff.threadAtCursor()
	diffFocused := r.isDiffFocused(m)

	edit := openEntry(r.hold.editorDesc(), m.keys.Review.EditFile, func(m *model) tea.Cmd { return m.review.pressEdit(m) })
	entries := []submenuEntry{drawnOff(edit, r.editOffSaid())}

	viewed := openEntry(r.diff.viewedDesc(), m.keys.Review.ToggleViewed, func(m *model) tea.Cmd { return m.review.toggleViewed(m) })
	entries = append(entries, drawnOff(viewed, r.diff.viewedOffSaid()))

	entries = append(entries,
		openEntry(r.diffKeys().expandDesc(), m.keys.Diff.Expand, func(m *model) tea.Cmd { return m.review.diffKeys().toggleExpanded(m) }),
		openEntry(wrapDesc(m), m.keys.Diff.Wrap, func(m *model) tea.Cmd { return m.review.diffKeys().toggleWrap(m) }),
	)

	if diffFocused && !onThread {
		if r.doesOfferAnswer() {
			entries = append(entries, r.answerEntry(m))
		}

		if r.diff.spec.origin != nil {
			entries = append(entries, r.jumpEntry(m))
		}

		if reachable {
			entries = append(entries, r.composeEntry(m, r.diff.newestSaid()))
		}
	}

	if r.isHistory() {
		entries = append(entries, openEntry(fmt.Sprintf("end the range on round %d", r.diff.newest.GetOrdinal()), m.keys.Review.RangeEnd, func(m *model) tea.Cmd {
			return m.review.endOnNewest(m)
		}))
	}

	if reachable {
		entries = append(entries, openEntry(r.threads.sendDesc(), m.keys.Review.Send, func(m *model) tea.Cmd {
			return m.review.orRefuseWrite(m, m.review.openRelease)
		}))
	}

	entries = append(entries, openEntry(r.reloadDesc(m), m.keys.Review.Reload, func(m *model) tea.Cmd { return m.review.catchUp(m) }))
	entries = append(entries, openEntry(r.diff.agentThreadsDesc(), m.keys.Review.AgentThreads, func(m *model) tea.Cmd { return m.review.toggleAgentThreads(m) }))

	return submenu{
		kind:    formMenu,
		title:   "review",
		entries: entries,
	}, true
}

func (r *reviewScreen) composeEntry(m *model, said string) submenuEntry {
	return drawnOff(openEntry(r.composeDesc(), m.keys.Review.Compose, func(m *model) tea.Cmd {
		return m.review.orRefuseWrite(m, m.review.compose)
	}), said)
}

func (r *reviewScreen) answerEntry(m *model) submenuEntry {
	return openEntry(r.diff.answerDesc(), m.keys.Review.Answer, func(m *model) tea.Cmd { return m.review.toggleAnswer(m) })
}

func (r *reviewScreen) jumpEntry(m *model) submenuEntry {
	return openEntry(r.diff.jumpDesc(), m.keys.Review.Jump, func(m *model) tea.Cmd { return m.review.toggleJump(m) })
}

// folderDesc says whether the folder key on the tree opens the folder or closes it.
func (r *reviewScreen) folderDesc() string {
	folder, _ := r.readCurrentFolder()
	if r.closedFolders[folder.Path] {
		return "open folder"
	}

	return "close folder"
}
