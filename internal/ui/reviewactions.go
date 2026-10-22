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
func (r *reviewScreen) actions(in screen.Inputs) keys.Spec[*reviewScreen] {
	switch {
	case r.isSideFocused(in):
		return r.sideActions(in)
	case r.isBlockFocused(in):
		return r.filesActions(in)
	}

	return r.diffActions(in)
}

// diffActions is the review screen as one table, walked in precedence order by the dispatch and
// named in reading order by a legend of the same ids.
func (r *reviewScreen) diffActions(in screen.Inputs) keys.Spec[*reviewScreen] {
	return keys.Spec[*reviewScreen]{
		Table:  r.table(in),
		Legend: r.legend(in),
	}
}

func (r *reviewScreen) table(in screen.Inputs) []keys.Action[*reviewScreen] {
	return slices.Concat(r.ownTable(in), diffTable[*reviewScreen](r.diffKeys(), in))
}

// ownTable is the review's keys past the ones every diff pane shares.
func (r *reviewScreen) ownTable(in screen.Inputs) []keys.Action[*reviewScreen] {
	return []keys.Action[*reviewScreen]{
		{
			ID:     actionReviewBack,
			Keys:   keys.FixedBack,
			Direct: keys.Directly(editorRelease, func(*reviewScreen, string) screen.Request { return r.backFromDiff(in) }),
		},
		{
			ID:   keys.Quit,
			Keys: keys.FixedQuit,
			Nav:  func(*reviewScreen, string) screen.Request { return screen.Hide{} },
		},
		{
			ID:   actionReviewThread,
			Keys: func(k config.Keybindings) []string { return slices.Concat(k.Review.NextThread, k.Review.PrevThread) },
			Nav:  func(_ *reviewScreen, pressed string) screen.Request { return r.stepThread(in, pressed) },
		},
		{
			ID: actionReviewOpenThread,
			Keys: func(k config.Keybindings) []string {
				return slices.Concat(k.Review.NextOpenThread, k.Review.PrevOpenThread)
			},
			Nav: func(_ *reviewScreen, pressed string) screen.Request { return r.stepOpenThread(in, pressed) },
		},
		{
			ID:   actionReviewBase,
			Keys: func(k config.Keybindings) []string { return k.Review.Base },
			When: (*reviewScreen).doesWalk,
			Nav:  func(*reviewScreen, string) screen.Request { return r.stepBase() },
		},
		{
			ID:   actionReviewBaseForward,
			Keys: func(k config.Keybindings) []string { return k.Review.BaseForward },
			When: (*reviewScreen).doesWalk,
			Nav:  func(*reviewScreen, string) screen.Request { return r.stepBaseForward() },
		},
	}
}

// orRefuseWrite runs fn, which puts something in front of the agent, or says why it did not, when
// the agent it would reach is no longer there.
func (r *reviewScreen) orRefuseWrite(in screen.Inputs, fn func(screen.Inputs) screen.Request) screen.Request {
	if r.isArchived(in) {
		return screen.NewNote(screen.Warning, archivedSaid)
	}

	return fn(in)
}

func (r *reviewScreen) legend(in screen.Inputs) []keys.LegendRow[*reviewScreen] {
	fitsBase := func(*reviewScreen) bool { return r.doesFitBaseLegend(in) }

	return []keys.LegendRow[*reviewScreen]{
		keys.MoveRow[*reviewScreen](),
		keys.NavPageRow[*reviewScreen](),
		diffFileRow[*reviewScreen](),
		{
			Of:   []keys.ID{actionReviewThread, actionReviewOpenThread},
			Desc: func(*reviewScreen) string { return r.diff.threadStepDesc() },
			Label: func(k config.Keybindings) string {
				return keys.HeadLabel(k.Review.NextThread) + "/" + keys.HeadLabel(k.Review.PrevThread) +
					"/" + keys.HeadLabel(k.Review.NextOpenThread) + "/" + keys.HeadLabel(k.Review.PrevOpenThread)
			},
		},
		diffSearchRow[*reviewScreen](),
		diffMatchRow[*reviewScreen](),
		diffPanRow[*reviewScreen](),
		keys.JumpRow[*reviewScreen](),
		{
			Of:   []keys.ID{actionReviewBase},
			Desc: func(*reviewScreen) string { return "an earlier review" },
			Show: fitsBase,
		},
		{
			Of:   []keys.ID{actionReviewBaseForward},
			Desc: func(*reviewScreen) string { return "a later review" },
			Show: fitsBase,
		},
		{
			Of:   []keys.ID{actionReviewBack},
			Desc: func(*reviewScreen) string { return r.diffBackDesc(in) },
		},
	}
}

// stepOpenThread walks the comments nobody has settled.
func (r *reviewScreen) stepOpenThread(in screen.Inputs, pressed string) screen.Request {
	if keys.IsBound(pressed, r.deps.keys.Review.PrevOpenThread) {
		return r.jumpOpenThread(in, -1)
	}

	return r.jumpOpenThread(in, 1)
}

func (r *reviewScreen) stepThread(in screen.Inputs, pressed string) screen.Request {
	if keys.IsBound(pressed, r.deps.keys.Review.PrevThread) {
		return r.moveCursor(in, r.diff.View().JumpThread(r.diff.Cursor(), -1))
	}

	return r.moveCursor(in, r.diff.View().JumpThread(r.diff.Cursor(), 1))
}

// filesActions is the keys on the file tree, which walk it a row at a time with the diff following.
func (r *reviewScreen) filesActions(in screen.Inputs) keys.Spec[*reviewScreen] {
	standing := func(*reviewScreen) bool { return r.filesFilter.isStanding() }

	return keys.Spec[*reviewScreen]{
		Table: append(navActions[*reviewScreen](navActionsParams{keys: r.deps.keys, nav: filesNav{r}, in: in, page: navPageStep, scroll: 1}), []keys.Action[*reviewScreen]{
			{
				ID:   actionDiffFile,
				Keys: func(k config.Keybindings) []string { return slices.Concat(k.Diff.NextFile, k.Diff.PrevFile) },
				Nav: func(_ *reviewScreen, pressed string) screen.Request {
					return r.stepFile(in, keys.DirectionOf(pressed, r.deps.keys.Diff.PrevFile))
				},
			},
			{
				ID: actionReviewFolder,
				Keys: func(k config.Keybindings) []string {
					return slices.Concat(keys.FixedOpen(k), k.Review.ToggleFolder)
				},
				Nav: func(*reviewScreen, string) screen.Request {
					r.toggleFolder()

					return nil
				},
			},
			{
				ID:   keys.FilterOpen,
				Keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
				Nav: func(*reviewScreen, string) screen.Request {
					r.openFilesFilter()

					return nil
				},
			},
			{
				ID:   keys.FilterMatch,
				Keys: func(k config.Keybindings) []string { return slices.Concat(k.Filter.NextMatch, k.Filter.PrevMatch) },
				When: standing,
				Nav: func(_ *reviewScreen, pressed string) screen.Request {
					return r.jumpFilesMatch(in, keys.DirectionOf(pressed, r.deps.keys.Filter.PrevMatch))
				},
			},
			{
				ID:   keys.FilterClear,
				Keys: keys.FixedBack,
				When: standing,
				Nav: func(*reviewScreen, string) screen.Request {
					r.filesFilter.abandon()

					return nil
				},
			},
			{
				ID:     actionReviewBack,
				Keys:   keys.FixedBack,
				Direct: keys.Directly(editorRelease, func(*reviewScreen, string) screen.Request { return r.back(in) }),
			},
			{
				ID:   keys.Quit,
				Keys: keys.FixedQuit,
				Nav:  func(*reviewScreen, string) screen.Request { return screen.Hide{} },
			},
		}...),
		Legend: []keys.LegendRow[*reviewScreen]{
			keys.MoveRow[*reviewScreen](),
			keys.NavPageRow[*reviewScreen](),
			keys.JumpRow[*reviewScreen](),
			diffFileRow[*reviewScreen](),
			{
				Of:   []keys.ID{actionReviewFolder},
				Desc: func(*reviewScreen) string { return r.folderDesc() },
				Show: func(*reviewScreen) bool {
					_, ok := r.readCurrentFolder()

					return ok
				},
			},
			{
				Of:   []keys.ID{keys.FilterOpen},
				Desc: func(*reviewScreen) string { return r.filesFilter.openDesc() },
			},
			{
				Of:   []keys.ID{keys.FilterMatch},
				Desc: func(*reviewScreen) string { return "next/prev match" },
			},
			{
				Of:   []keys.ID{keys.FilterClear},
				Desc: func(*reviewScreen) string { return r.filesFilter.clearDesc() },
			},
			{
				Of:    []keys.ID{actionReviewBack},
				Desc:  func(*reviewScreen) string { return r.backDesc() },
				Show:  func(*reviewScreen) bool { return !r.filesFilter.isStanding() },
				Strip: true,
			},
		},
	}
}

// sideActions is the keys on the rows over the file tree, which walk them and on down into the tree.
func (r *reviewScreen) sideActions(in screen.Inputs) keys.Spec[*reviewScreen] {
	return keys.Spec[*reviewScreen]{
		Table: append(navActions[*reviewScreen](navActionsParams{keys: r.deps.keys, nav: filesNav{r}, in: in, page: navPageStep, scroll: 1}), []keys.Action[*reviewScreen]{
			{
				ID: actionReviewSide,
				Keys: func(k config.Keybindings) []string {
					return slices.Concat(keys.FixedOpen(k), k.Review.ToggleFolder)
				},
				Nav: func(*reviewScreen, string) screen.Request { return r.chooseSide(in) },
			},
			{
				ID:     actionReviewBack,
				Keys:   keys.FixedBack,
				Direct: keys.Directly(editorRelease, func(*reviewScreen, string) screen.Request { return r.back(in) }),
			},
			{
				ID:   keys.Quit,
				Keys: keys.FixedQuit,
				Nav:  func(*reviewScreen, string) screen.Request { return screen.Hide{} },
			},
		}...),
		Legend: []keys.LegendRow[*reviewScreen]{
			keys.MoveRow[*reviewScreen](),
			keys.NavPageRow[*reviewScreen](),
			keys.JumpRow[*reviewScreen](),
			{
				Of:   []keys.ID{actionReviewSide},
				Desc: func(*reviewScreen) string { return r.chooseDesc(in) },
				Show: func(*reviewScreen) bool { return r.chooseDesc(in) != "" },
			},
			{
				Of:    []keys.ID{actionReviewBack},
				Desc:  func(*reviewScreen) string { return r.backDesc() },
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
func (r *reviewScreen) buildItemMenu(in screen.Inputs, _ menu.Nester) (menu.Menu, bool) {
	if side, ok := r.findSide(in); ok && r.isBlockFocused(in) {
		return r.sideMenu(in, side)
	}

	if !r.isDiffFocused(in) {
		return menu.Menu{}, false
	}

	_, ok := r.diff.threadAtCursor()
	if !ok {
		return menu.Menu{}, false
	}

	reachable := !r.isArchived(in)
	entries := []menu.Entry{}

	if reachable {
		entries = append(entries, r.composeEntry(""))
	}

	if reachable && r.doesOfferForgeReply() {
		entries = append(entries, menu.Leaving("reply on "+r.forgeName(in), r.deps.keys.Review.ForgeReply, func(in screen.Inputs) screen.Request {
			return r.orRefuseWrite(in, r.composeForge)
		}))
	}

	entries = append(entries, menu.Leaving(r.diff.resolveDesc(), r.deps.keys.Review.Resolve, func(in screen.Inputs) screen.Request { return r.resolveThread(in) }))

	if r.doesOfferAccept() {
		accept := menu.Leaving("accept the forge comment", r.deps.keys.Review.Accept, func(in screen.Inputs) screen.Request {
			return r.orRefuseWrite(in, r.acceptForge)
		})
		entries = append(entries, menu.DrawnOff(accept, r.diff.newestSaid()))
	}

	if reachable && r.doesOfferForgeRetry() {
		entries = append(entries,
			menu.Leaving("retry the reply", r.deps.keys.Review.ForgePost, func(in screen.Inputs) screen.Request {
				return r.orRefuseWrite(in, r.retryForgeReply)
			}),
			menu.Leaving("discard the reply", r.deps.keys.Review.ForgeDiscard, func(in screen.Inputs) screen.Request {
				return r.orRefuseWrite(in, r.discardForgeReply)
			}),
		)
	}

	if r.doesOfferAnswer() {
		entries = append(entries, r.answerEntry())
	}

	entries = append(entries, menu.DrawnOff(r.jumpEntry(), r.diff.jumpOffSaid()))

	return menu.Menu{
		Title:   "thread",
		Entries: entries,
	}, true
}

// buildScreenMenu builds the band of whichever of the diff, the file tree and the rows over it holds
// the keys.
func (r *reviewScreen) buildScreenMenu(in screen.Inputs, nest menu.Nester) (menu.Menu, bool) {
	var band menu.Menu
	switch {
	case r.isSideFocused(in):
		band = pullRequestMenu()
	case r.isBlockFocused(in):
		band = menu.Menu{Title: "file", Entries: r.fileEntries(in)}
	default:
		band = r.diffMenu(in, nest)
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
func (r *reviewScreen) fileEntries(in screen.Inputs) []menu.Entry {
	edit := menu.Leaving(r.edit.editorDesc(), r.deps.keys.Review.EditFile, r.pressEdit)
	viewed := menu.Leaving(r.diff.viewedDesc(), r.deps.keys.Review.ToggleViewed, r.toggleViewed)

	return []menu.Entry{menu.DrawnOff(edit, r.editOffSaid(in)), menu.DrawnOff(viewed, r.diff.viewedOffSaid())}
}

// diffMenu is the diff's band, for the file in the editor, how the diff is rendered and the review
// waiting to be sent.
func (r *reviewScreen) diffMenu(in screen.Inputs, nest menu.Nester) menu.Menu {
	reachable := !r.isArchived(in)
	_, onThread := r.diff.threadAtCursor()
	diffFocused := r.isDiffFocused(in)

	entries := append(r.fileEntries(in),
		menu.Leaving(r.diffKeys().expandDesc(), r.deps.keys.Diff.Expand, func(in screen.Inputs) screen.Request { return r.diffKeys().toggleExpanded(in) }),
		menu.Leaving(wrapDesc(in), r.deps.keys.Diff.Wrap, func(in screen.Inputs) screen.Request { return r.diffKeys().toggleWrap(in) }),
	)

	if diffFocused && !onThread {
		if r.doesOfferAnswer() {
			entries = append(entries, r.answerEntry())
		}

		if r.diff.spec.origin != nil {
			entries = append(entries, r.jumpEntry())
		}

		if reachable {
			entries = append(entries, r.composeEntry(r.diff.newestSaid()))
		}
	}

	if r.isHistory() {
		entries = append(entries, menu.Leaving(fmt.Sprintf("end the range on snapshot %d", r.diff.newest.GetOrdinal()), r.deps.keys.Review.RangeEnd, func(screen.Inputs) screen.Request {
			return r.endOnNewest()
		}))
	}

	entries = append(entries, menu.Leaving(r.reloadDesc(in), r.deps.keys.Review.Reload, func(screen.Inputs) screen.Request { return r.catchUp() }))
	entries = append(entries, menu.Leaving(r.diff.agentThreadsDesc(), r.deps.keys.Review.AgentThreads, r.toggleAgentThreads))

	if task, ok := in.Listing.Tasks[r.taskID]; ok {
		entries = append(entries, menu.Nested("git", r.deps.keys.Menu.Git, nest.PullRequestGit(task)))
	}

	return menu.Menu{
		Title:   "review",
		Entries: entries,
	}
}

func (r *reviewScreen) composeEntry(said string) menu.Entry {
	return menu.DrawnOff(menu.Leaving(r.composeDesc(), r.deps.keys.Review.Compose, func(in screen.Inputs) screen.Request {
		return r.orRefuseWrite(in, r.compose)
	}), said)
}

func (r *reviewScreen) answerEntry() menu.Entry {
	return menu.Leaving(r.diff.answerDesc(), r.deps.keys.Review.Answer, func(screen.Inputs) screen.Request { return r.toggleAnswer() })
}

func (r *reviewScreen) jumpEntry() menu.Entry {
	return menu.Leaving(r.diff.jumpDesc(), r.deps.keys.Review.Jump, func(screen.Inputs) screen.Request { return r.toggleJump() })
}

// folderDesc says whether the folder key on the tree opens the folder or closes it.
func (r *reviewScreen) folderDesc() string {
	folder, _ := r.readCurrentFolder()
	if r.closedFolders[folder.Path] {
		return "open folder"
	}

	return "close folder"
}
