package ui

import (
	"fmt"
	"slices"

	"komrad/config"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/screen"
)

// actions is the keys of whichever of the diff, the file tree and the rows over it holds them.
func (r *reviewScreen) actions(m *model) keys.Spec[*model] {
	switch in := m.inputs(); {
	case r.isSideFocused(m, in):
		return r.sideActions()
	case r.isBlockFocused(m, in):
		return r.filesActions()
	}

	return r.diffActions()
}

// diffActions is the review screen as one table, walked in precedence order by the dispatch and
// named in reading order by a legend of the same ids.
func (r *reviewScreen) diffActions() keys.Spec[*model] {
	return keys.Spec[*model]{
		Table:  r.table(),
		Legend: r.legend(),
	}
}

func (r *reviewScreen) table() []keys.Action[*model] {
	return slices.Concat(r.ownTable(), r.diffKeys().table())
}

// ownTable is the review's keys past the ones every diff pane shares.
func (r *reviewScreen) ownTable() []keys.Action[*model] {
	return []keys.Action[*model]{
		{
			ID:     actionReviewBack,
			Keys:   keys.FixedBack,
			Direct: keys.Directly(editorRelease, func(m *model, _ string) screen.Request { return r.backFromDiff(m, m.inputs()) }),
		},
		{
			ID:   keys.Quit,
			Keys: keys.FixedQuit,
			Nav:  func(m *model, _ string) screen.Request { return screen.Hide{} },
		},
		{
			ID:   actionReviewThread,
			Keys: func(k config.Keybindings) []string { return slices.Concat(k.Review.NextThread, k.Review.PrevThread) },
			Nav:  r.stepThread,
		},
		{
			ID: actionReviewOpenThread,
			Keys: func(k config.Keybindings) []string {
				return slices.Concat(k.Review.NextOpenThread, k.Review.PrevOpenThread)
			},
			Nav: r.stepOpenThread,
		},
		{
			ID:   actionReviewBase,
			Keys: func(k config.Keybindings) []string { return k.Review.Base },
			When: func(*model) bool { return r.doesWalk() },
			Nav:  func(*model, string) screen.Request { return r.stepBase() },
		},
		{
			ID:   actionReviewBaseForward,
			Keys: func(k config.Keybindings) []string { return k.Review.BaseForward },
			When: func(*model) bool { return r.doesWalk() },
			Nav:  func(*model, string) screen.Request { return r.stepBaseForward() },
		},
	}
}

// orRefuseWrite runs fn, which puts something in front of the agent, or says why it did not, when
// the agent it would reach is no longer there.
func (r *reviewScreen) orRefuseWrite(m *model, fn func(*model) screen.Request) screen.Request {
	if r.isArchived(m) {
		return screen.NewNote(screen.Warning, archivedSaid)
	}

	return fn(m)
}

func (r *reviewScreen) legend() []keys.LegendRow[*model] {
	return []keys.LegendRow[*model]{
		keys.MoveRow[*model](),
		keys.NavPageRow[*model](),
		diffFileRow(),
		{
			Of:   []keys.ID{actionReviewThread, actionReviewOpenThread},
			Desc: func(*model) string { return r.diff.threadStepDesc() },
			Label: func(k config.Keybindings) string {
				return keys.HeadLabel(k.Review.NextThread) + "/" + keys.HeadLabel(k.Review.PrevThread) +
					"/" + keys.HeadLabel(k.Review.NextOpenThread) + "/" + keys.HeadLabel(k.Review.PrevOpenThread)
			},
		},
		diffSearchRow(),
		diffMatchRow(),
		diffPanRow(),
		keys.JumpRow[*model](),
		{
			Of:   []keys.ID{actionReviewBase},
			Desc: func(*model) string { return "an earlier review" },
			Show: r.doesFitBaseLegend,
		},
		{
			Of:   []keys.ID{actionReviewBaseForward},
			Desc: func(*model) string { return "a later review" },
			Show: r.doesFitBaseLegend,
		},
		{
			Of:   []keys.ID{actionReviewBack},
			Desc: func(m *model) string { return r.diffBackDesc(m, m.inputs()) },
		},
	}
}

// stepOpenThread walks the comments nobody has settled.
func (r *reviewScreen) stepOpenThread(m *model, pressed string) screen.Request {
	if keys.IsBound(pressed, r.deps.keys.Review.PrevOpenThread) {
		return r.jumpOpenThread(m, -1)
	}

	return r.jumpOpenThread(m, 1)
}

func (r *reviewScreen) stepThread(m *model, pressed string) screen.Request {
	if keys.IsBound(pressed, r.deps.keys.Review.PrevThread) {
		return r.moveCursor(m, r.diff.View().JumpThread(r.diff.Cursor(), -1))
	}

	return r.moveCursor(m, r.diff.View().JumpThread(r.diff.Cursor(), 1))
}

// filesActions is the keys on the file tree, which walk it a row at a time with the diff following.
func (r *reviewScreen) filesActions() keys.Spec[*model] {
	standing := func(*model) bool { return r.filesFilter.isStanding() }

	return keys.Spec[*model]{
		Table: append(navActions(filesNav{r}, navSteps{page: navPageStep, scroll: 1}), []keys.Action[*model]{
			{
				ID:   actionDiffFile,
				Keys: func(k config.Keybindings) []string { return slices.Concat(k.Diff.NextFile, k.Diff.PrevFile) },
				Nav: func(m *model, pressed string) screen.Request {
					return r.stepFile(m, keys.DirectionOf(pressed, r.deps.keys.Diff.PrevFile))
				},
			},
			{
				ID: actionReviewFolder,
				Keys: func(k config.Keybindings) []string {
					return slices.Concat(keys.FixedOpen(k), k.Review.ToggleFolder)
				},
				Nav: func(*model, string) screen.Request {
					r.toggleFolder()

					return nil
				},
			},
			{
				ID:   keys.FilterOpen,
				Keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
				Nav: func(*model, string) screen.Request {
					r.openFilesFilter()

					return nil
				},
			},
			{
				ID:   keys.FilterMatch,
				Keys: func(k config.Keybindings) []string { return slices.Concat(k.Filter.NextMatch, k.Filter.PrevMatch) },
				When: standing,
				Nav: func(m *model, pressed string) screen.Request {
					return r.jumpFilesMatch(m, keys.DirectionOf(pressed, r.deps.keys.Filter.PrevMatch))
				},
			},
			{
				ID:   keys.FilterClear,
				Keys: keys.FixedBack,
				When: standing,
				Nav: func(*model, string) screen.Request {
					r.filesFilter.abandon()

					return nil
				},
			},
			{
				ID:     actionReviewBack,
				Keys:   keys.FixedBack,
				Direct: keys.Directly(editorRelease, func(m *model, _ string) screen.Request { return r.back(m) }),
			},
			{
				ID:   keys.Quit,
				Keys: keys.FixedQuit,
				Nav:  func(m *model, _ string) screen.Request { return screen.Hide{} },
			},
		}...),
		Legend: []keys.LegendRow[*model]{
			keys.MoveRow[*model](),
			keys.NavPageRow[*model](),
			keys.JumpRow[*model](),
			diffFileRow(),
			{
				Of:   []keys.ID{actionReviewFolder},
				Desc: func(*model) string { return r.folderDesc() },
				Show: func(*model) bool {
					_, ok := r.readCurrentFolder()

					return ok
				},
			},
			{
				Of:   []keys.ID{keys.FilterOpen},
				Desc: func(*model) string { return r.filesFilter.openDesc() },
			},
			{
				Of:   []keys.ID{keys.FilterMatch},
				Desc: func(*model) string { return "next/prev match" },
			},
			{
				Of:   []keys.ID{keys.FilterClear},
				Desc: func(*model) string { return r.filesFilter.clearDesc() },
			},
			{
				Of:    []keys.ID{actionReviewBack},
				Desc:  func(*model) string { return r.backDesc() },
				Show:  func(*model) bool { return !r.filesFilter.isStanding() },
				Strip: true,
			},
		},
	}
}

// sideActions is the keys on the rows over the file tree, which walk them and on down into the tree.
func (r *reviewScreen) sideActions() keys.Spec[*model] {
	return keys.Spec[*model]{
		Table: append(navActions(filesNav{r}, navSteps{page: navPageStep, scroll: 1}), []keys.Action[*model]{
			{
				ID: actionReviewSide,
				Keys: func(k config.Keybindings) []string {
					return slices.Concat(keys.FixedOpen(k), k.Review.ToggleFolder)
				},
				Nav: func(m *model, _ string) screen.Request { return r.chooseSide(m, m.inputs()) },
			},
			{
				ID:     actionReviewBack,
				Keys:   keys.FixedBack,
				Direct: keys.Directly(editorRelease, func(m *model, _ string) screen.Request { return r.back(m) }),
			},
			{
				ID:   keys.Quit,
				Keys: keys.FixedQuit,
				Nav:  func(m *model, _ string) screen.Request { return screen.Hide{} },
			},
		}...),
		Legend: []keys.LegendRow[*model]{
			keys.MoveRow[*model](),
			keys.NavPageRow[*model](),
			keys.JumpRow[*model](),
			{
				Of:   []keys.ID{actionReviewSide},
				Desc: func(m *model) string { return r.chooseDesc(m) },
				Show: func(m *model) bool { return r.chooseDesc(m) != "" },
			},
			{
				Of:    []keys.ID{actionReviewBack},
				Desc:  func(*model) string { return r.backDesc() },
				Strip: true,
			},
		},
	}
}

// The review's own reasons for an arm that acts straight off its key.
const (
	editorRelease = "leaving the editor releases the agent whichever key leaves it"
)

// buildItemMenu builds the band on the thread under the diff's cursor.
func (r *reviewScreen) buildItemMenu(m *model, in screen.Inputs) (menu.Menu, bool) {
	if side, ok := r.findSide(m); ok && r.isBlockFocused(m, in) {
		return r.sideMenu(m, in, side)
	}

	if !r.isDiffFocused(m, in) {
		return menu.Menu{}, false
	}

	_, ok := r.diff.threadAtCursor()
	if !ok {
		return menu.Menu{}, false
	}

	reachable := !r.isArchived(m)
	entries := []menu.Entry{}

	if reachable {
		entries = append(entries, r.composeEntry(m, ""))
	}

	if reachable && r.doesOfferForgeReply() {
		entries = append(entries, menu.Leaving("reply on "+r.forgeName(m), r.deps.keys.Review.ForgeReply, func() screen.Request {
			return m.review.orRefuseWrite(m, m.review.composeForge)
		}))
	}

	entries = append(entries, menu.Leaving(r.diff.resolveDesc(), r.deps.keys.Review.Resolve, func() screen.Request { return m.review.resolveThread(m) }))

	if r.doesOfferAccept() {
		accept := menu.Leaving("accept the forge comment", r.deps.keys.Review.Accept, func() screen.Request {
			return m.review.orRefuseWrite(m, m.review.acceptForge)
		})
		entries = append(entries, menu.DrawnOff(accept, r.diff.newestSaid()))
	}

	if reachable && r.doesOfferForgeRetry() {
		entries = append(entries,
			menu.Leaving("retry the reply", r.deps.keys.Review.ForgePost, func() screen.Request {
				return m.review.orRefuseWrite(m, m.review.retryForgeReply)
			}),
			menu.Leaving("discard the reply", r.deps.keys.Review.ForgeDiscard, func() screen.Request {
				return m.review.orRefuseWrite(m, m.review.discardForgeReply)
			}),
		)
	}

	if r.doesOfferAnswer() {
		entries = append(entries, r.answerEntry(m))
	}

	entries = append(entries, menu.DrawnOff(r.jumpEntry(m), r.diff.jumpOffSaid()))

	return menu.Menu{
		Title:   "thread",
		Entries: entries,
	}, true
}

// buildScreenMenu builds the band of whichever of the diff, the file tree and the rows over it holds
// the keys.
func (r *reviewScreen) buildScreenMenu(m *model, in screen.Inputs) (menu.Menu, bool) {
	var band menu.Menu
	switch {
	case r.isSideFocused(m, in):
		band = pullRequestMenu()
	case r.isBlockFocused(m, in):
		band = menu.Menu{Title: "file", Entries: r.fileEntries(m)}
	default:
		band = r.diffMenu(m, in)
	}

	return band, true
}

// pullRequestMenu is the band on the rows over the file tree, which offers nothing yet.
func pullRequestMenu() menu.Menu {
	return menu.Menu{
		Title:   "pull request",
		Entries: []menu.Entry{menu.DrawnOff(menu.Entry{Label: "edit the title and description"}, "not yet")},
	}
}

// fileEntries is the entries on the file under the cursor, for the editor and whether it is viewed.
func (r *reviewScreen) fileEntries(m *model) []menu.Entry {
	edit := menu.Leaving(r.hold.editorDesc(), r.deps.keys.Review.EditFile, func() screen.Request { return m.review.pressEdit(m) })
	viewed := menu.Leaving(r.diff.viewedDesc(), r.deps.keys.Review.ToggleViewed, func() screen.Request { return m.review.toggleViewed(m) })

	return []menu.Entry{menu.DrawnOff(edit, r.editOffSaid(m)), menu.DrawnOff(viewed, r.diff.viewedOffSaid())}
}

// diffMenu is the diff's band, for the file in the editor, how the diff is rendered and the review
// waiting to be sent.
func (r *reviewScreen) diffMenu(m *model, in screen.Inputs) menu.Menu {
	reachable := !r.isArchived(m)
	_, onThread := r.diff.threadAtCursor()
	diffFocused := r.isDiffFocused(m, in)

	entries := append(r.fileEntries(m),
		menu.Leaving(r.diffKeys().expandDesc(), r.deps.keys.Diff.Expand, func() screen.Request { return m.review.diffKeys().toggleExpanded(m) }),
		menu.Leaving(wrapDesc(m), r.deps.keys.Diff.Wrap, func() screen.Request { return m.review.diffKeys().toggleWrap(m) }),
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
		entries = append(entries, menu.Leaving(fmt.Sprintf("end the range on snapshot %d", r.diff.newest.GetOrdinal()), r.deps.keys.Review.RangeEnd, func() screen.Request {
			return m.review.endOnNewest()
		}))
	}

	entries = append(entries, menu.Leaving(r.reloadDesc(m), r.deps.keys.Review.Reload, func() screen.Request { return m.review.catchUp() }))
	entries = append(entries, menu.Leaving(r.diff.agentThreadsDesc(), r.deps.keys.Review.AgentThreads, func() screen.Request { return m.review.toggleAgentThreads(m) }))

	if task, ok := in.Listing.Tasks[r.taskID]; ok {
		entries = append(entries, menu.Nested("git", r.deps.keys.Menu.Git, pullRequestGitMenu(m, task)))
	}

	return menu.Menu{
		Title:   "review",
		Entries: entries,
	}
}

func (r *reviewScreen) composeEntry(m *model, said string) menu.Entry {
	return menu.DrawnOff(menu.Leaving(r.composeDesc(), r.deps.keys.Review.Compose, func() screen.Request {
		return m.review.orRefuseWrite(m, m.review.compose)
	}), said)
}

func (r *reviewScreen) answerEntry(m *model) menu.Entry {
	return menu.Leaving(r.diff.answerDesc(), r.deps.keys.Review.Answer, func() screen.Request { return m.review.toggleAnswer() })
}

func (r *reviewScreen) jumpEntry(m *model) menu.Entry {
	return menu.Leaving(r.diff.jumpDesc(), r.deps.keys.Review.Jump, func() screen.Request { return m.review.toggleJump() })
}

// folderDesc says whether the folder key on the tree opens the folder or closes it.
func (r *reviewScreen) folderDesc() string {
	folder, _ := r.readCurrentFolder()
	if r.closedFolders[folder.Path] {
		return "open folder"
	}

	return "close folder"
}
