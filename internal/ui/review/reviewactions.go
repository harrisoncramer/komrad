package review

import (
	"fmt"
	"slices"

	"komrad/config"
	"komrad/internal/ui/filter"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/screen"
)

const (
	actionBack keys.ID = keys.Own + iota
	actionThread
	actionOpenThread
	actionBase
	actionBaseForward
	actionFolder
	actionSide
)

// actions is the keys of whichever of the diff, the file tree and the rows over it holds them.
func (r *Screen) actions(in screen.Inputs) keys.Spec[*Screen] {
	switch {
	case r.isSideFocused(in):
		return r.sideActions(in)
	case r.IsBlockFocused(in):
		return r.filesActions(in)
	}

	return r.diffActions(in)
}

// diffActions is the review screen as one table, walked in precedence order by the dispatch and
// named in reading order by a legend of the same ids.
func (r *Screen) diffActions(in screen.Inputs) keys.Spec[*Screen] {
	return keys.Spec[*Screen]{
		Table:  r.table(in),
		Legend: r.legend(in),
	}
}

func (r *Screen) table(in screen.Inputs) []keys.Action[*Screen] {
	return slices.Concat(r.ownTable(in), keys.DiffTable[*Screen](r.diffKeys(), in))
}

// ownTable is the review's keys past the ones every diff pane shares.
func (r *Screen) ownTable(in screen.Inputs) []keys.Action[*Screen] {
	return []keys.Action[*Screen]{
		{
			ID:     actionBack,
			Keys:   keys.FixedBack,
			Direct: keys.Directly(editorRelease, func(*Screen, string) screen.Request { return r.backFromDiff(in) }),
		},
		{
			ID:   keys.Quit,
			Keys: keys.FixedQuit,
			Nav:  func(*Screen, string) screen.Request { return screen.Hide{} },
		},
		{
			ID:   actionThread,
			Keys: func(k config.Keybindings) []string { return slices.Concat(k.Review.NextThread, k.Review.PrevThread) },
			Nav:  func(_ *Screen, pressed string) screen.Request { return r.stepThread(in, pressed) },
		},
		{
			ID: actionOpenThread,
			Keys: func(k config.Keybindings) []string {
				return slices.Concat(k.Review.NextOpenThread, k.Review.PrevOpenThread)
			},
			Nav: func(_ *Screen, pressed string) screen.Request { return r.stepOpenThread(in, pressed) },
		},
		{
			ID:   actionBase,
			Keys: func(k config.Keybindings) []string { return k.Review.Base },
			When: (*Screen).doesWalk,
			Nav:  func(*Screen, string) screen.Request { return r.stepBase() },
		},
		{
			ID:   actionBaseForward,
			Keys: func(k config.Keybindings) []string { return k.Review.BaseForward },
			When: (*Screen).doesWalk,
			Nav:  func(*Screen, string) screen.Request { return r.stepBaseForward() },
		},
	}
}

// orRefuseWrite runs fn, which puts something in front of the agent, or says why it did not, when
// the agent it would reach is no longer there.
func (r *Screen) orRefuseWrite(in screen.Inputs, fn func(screen.Inputs) screen.Request) screen.Request {
	if r.isArchived(in) {
		return screen.NewPostMessage(screen.Warning, archivedSaid)
	}

	return fn(in)
}

func (r *Screen) legend(in screen.Inputs) []keys.LegendRow[*Screen] {
	fitsBase := func(*Screen) bool { return r.doesFitBaseLegend(in) }

	return []keys.LegendRow[*Screen]{
		keys.MoveRow[*Screen](),
		keys.NavPageRow[*Screen](),
		keys.DiffFileRow[*Screen](),
		{
			ActionIDs: []keys.ID{actionThread, actionOpenThread},
			Desc:      func(*Screen) string { return r.diff.threadStepDesc() },
			Label: func(k config.Keybindings) string {
				return keys.HeadLabel(k.Review.NextThread) + "/" + keys.HeadLabel(k.Review.PrevThread) +
					"/" + keys.HeadLabel(k.Review.NextOpenThread) + "/" + keys.HeadLabel(k.Review.PrevOpenThread)
			},
		},
		keys.DiffSearchRow[*Screen](),
		keys.DiffMatchRow[*Screen](),
		keys.DiffPanRow[*Screen](),
		keys.JumpRow[*Screen](),
		{
			ActionIDs: []keys.ID{actionBase},
			Desc:      func(*Screen) string { return "an earlier review" },
			Show:      fitsBase,
		},
		{
			ActionIDs: []keys.ID{actionBaseForward},
			Desc:      func(*Screen) string { return "a later review" },
			Show:      fitsBase,
		},
		{
			ActionIDs: []keys.ID{actionBack},
			Desc:      func(*Screen) string { return r.diffBackDesc(in) },
		},
	}
}

// stepOpenThread walks the comments nobody has settled.
func (r *Screen) stepOpenThread(in screen.Inputs, pressed string) screen.Request {
	if keys.IsBound(pressed, r.deps.Keys.Review.PrevOpenThread) {
		return r.jumpOpenThread(in, -1)
	}

	return r.jumpOpenThread(in, 1)
}

func (r *Screen) stepThread(in screen.Inputs, pressed string) screen.Request {
	if keys.IsBound(pressed, r.deps.Keys.Review.PrevThread) {
		return r.moveCursor(in, r.diff.View().JumpThread(r.diff.Cursor(), -1))
	}

	return r.moveCursor(in, r.diff.View().JumpThread(r.diff.Cursor(), 1))
}

// filesActions is the keys on the file tree, which walk it a row at a time with the diff following.
func (r *Screen) filesActions(in screen.Inputs) keys.Spec[*Screen] {
	standing := func(*Screen) bool { return r.filesFilter.IsStanding() }

	return keys.Spec[*Screen]{
		Table: append(keys.NavActions[*Screen](keys.NavActionsParams{Keys: r.deps.Keys, Nav: filesNav{r}, In: in, Page: keys.NavPageStep, Scroll: 1}), []keys.Action[*Screen]{
			{
				ID:   keys.DiffFile,
				Keys: func(k config.Keybindings) []string { return slices.Concat(k.Diff.NextFile, k.Diff.PrevFile) },
				Nav: func(_ *Screen, pressed string) screen.Request {
					return r.stepFile(in, keys.DirectionOf(pressed, r.deps.Keys.Diff.PrevFile))
				},
			},
			{
				ID: actionFolder,
				Keys: func(k config.Keybindings) []string {
					return slices.Concat(keys.FixedOpen(k), k.Review.ToggleFolder)
				},
				Nav: func(*Screen, string) screen.Request {
					r.toggleFolder()

					return nil
				},
			},
			{
				ID:   keys.FilterOpen,
				Keys: func(config.Keybindings) []string { return []string{filter.OpenKey} },
				Nav: func(*Screen, string) screen.Request {
					r.openFilesFilter()

					return nil
				},
			},
			{
				ID:   keys.FilterMatch,
				Keys: func(k config.Keybindings) []string { return slices.Concat(k.Filter.NextMatch, k.Filter.PrevMatch) },
				When: standing,
				Nav: func(_ *Screen, pressed string) screen.Request {
					return r.jumpFilesMatch(in, keys.DirectionOf(pressed, r.deps.Keys.Filter.PrevMatch))
				},
			},
			{
				ID:   keys.FilterClear,
				Keys: keys.FixedBack,
				When: standing,
				Nav: func(*Screen, string) screen.Request {
					r.filesFilter.Abandon()

					return nil
				},
			},
			{
				ID:     actionBack,
				Keys:   keys.FixedBack,
				Direct: keys.Directly(editorRelease, func(*Screen, string) screen.Request { return r.back(in) }),
			},
			{
				ID:   keys.Quit,
				Keys: keys.FixedQuit,
				Nav:  func(*Screen, string) screen.Request { return screen.Hide{} },
			},
		}...),
		Legend: []keys.LegendRow[*Screen]{
			keys.MoveRow[*Screen](),
			keys.NavPageRow[*Screen](),
			keys.JumpRow[*Screen](),
			keys.DiffFileRow[*Screen](),
			{
				ActionIDs: []keys.ID{actionFolder},
				Desc:      func(*Screen) string { return r.folderDesc() },
				Show: func(*Screen) bool {
					_, ok := r.readCurrentFolder()

					return ok
				},
			},
			{
				ActionIDs: []keys.ID{keys.FilterOpen},
				Desc:      func(*Screen) string { return r.filesFilter.OpenDesc() },
			},
			{
				ActionIDs: []keys.ID{keys.FilterMatch},
				Desc:      func(*Screen) string { return "next/prev match" },
			},
			{
				ActionIDs: []keys.ID{keys.FilterClear},
				Desc:      func(*Screen) string { return r.filesFilter.ClearDesc() },
			},
			{
				ActionIDs:     []keys.ID{actionBack},
				Desc:          func(*Screen) string { return r.backDesc() },
				Show:          func(*Screen) bool { return !r.filesFilter.IsStanding() },
				IsInShortHelp: true,
			},
		},
	}
}

// sideActions is the keys on the rows over the file tree, which walk them and on down into the tree.
func (r *Screen) sideActions(in screen.Inputs) keys.Spec[*Screen] {
	return keys.Spec[*Screen]{
		Table: append(keys.NavActions[*Screen](keys.NavActionsParams{Keys: r.deps.Keys, Nav: filesNav{r}, In: in, Page: keys.NavPageStep, Scroll: 1}), []keys.Action[*Screen]{
			{
				ID: actionSide,
				Keys: func(k config.Keybindings) []string {
					return slices.Concat(keys.FixedOpen(k), k.Review.ToggleFolder)
				},
				Nav: func(*Screen, string) screen.Request { return r.chooseSide(in) },
			},
			{
				ID:     actionBack,
				Keys:   keys.FixedBack,
				Direct: keys.Directly(editorRelease, func(*Screen, string) screen.Request { return r.back(in) }),
			},
			{
				ID:   keys.Quit,
				Keys: keys.FixedQuit,
				Nav:  func(*Screen, string) screen.Request { return screen.Hide{} },
			},
		}...),
		Legend: []keys.LegendRow[*Screen]{
			keys.MoveRow[*Screen](),
			keys.NavPageRow[*Screen](),
			keys.JumpRow[*Screen](),
			{
				ActionIDs: []keys.ID{actionSide},
				Desc:      func(*Screen) string { return r.chooseDesc(in) },
				Show:      func(*Screen) bool { return r.chooseDesc(in) != "" },
			},
			{
				ActionIDs:     []keys.ID{actionBack},
				Desc:          func(*Screen) string { return r.backDesc() },
				IsInShortHelp: true,
			},
		},
	}
}

// The review's own reasons for an arm that acts straight off its key.
const (
	editorRelease = "leaving the editor releases the agent whichever key leaves it"
)

// BuildItemMenu builds the band on the thread under the diff's cursor.
func (r *Screen) BuildItemMenu(in screen.Inputs, _ menu.NestedMenuBuilder) (menu.Menu, bool) {
	if side, ok := r.findSide(in); ok && r.IsBlockFocused(in) {
		return r.sideMenu(in, side)
	}

	if !r.IsDiffFocused(in) {
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
		entries = append(entries, menu.Leaving("reply on "+r.forgeName(in), r.deps.Keys.Review.ForgeReply, func(in screen.Inputs) screen.Request {
			return r.orRefuseWrite(in, r.composeForge)
		}))
	}

	entries = append(entries, menu.Leaving(r.diff.resolveDesc(), r.deps.Keys.Review.Resolve, func(in screen.Inputs) screen.Request { return r.resolveThread(in) }))

	if r.doesOfferAccept() {
		accept := menu.Leaving("accept the forge comment", r.deps.Keys.Review.Accept, func(in screen.Inputs) screen.Request {
			return r.orRefuseWrite(in, r.acceptForge)
		})
		entries = append(entries, menu.DrawnOff(accept, r.diff.newestSaid()))
	}

	if reachable && r.doesOfferForgeRetry() {
		entries = append(entries,
			menu.Leaving("retry the reply", r.deps.Keys.Review.ForgePost, func(in screen.Inputs) screen.Request {
				return r.orRefuseWrite(in, r.retryForgeReply)
			}),
			menu.Leaving("discard the reply", r.deps.Keys.Review.ForgeDiscard, func(in screen.Inputs) screen.Request {
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

// BuildScreenMenu builds the band of whichever of the diff, the file tree and the rows over it holds
// the keys.
func (r *Screen) BuildScreenMenu(in screen.Inputs, nest menu.NestedMenuBuilder) (menu.Menu, bool) {
	var band menu.Menu
	switch {
	case r.isSideFocused(in):
		band = pullRequestMenu()
	case r.IsBlockFocused(in):
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
func (r *Screen) fileEntries(in screen.Inputs) []menu.Entry {
	edit := menu.Leaving(r.edit.editorDesc(), r.deps.Keys.Review.EditFile, r.pressEdit)
	viewed := menu.Leaving(r.diff.viewedDesc(), r.deps.Keys.Review.ToggleViewed, r.toggleViewed)

	return []menu.Entry{menu.DrawnOff(edit, r.editOffSaid(in)), menu.DrawnOff(viewed, r.diff.viewedOffSaid())}
}

// diffMenu is the diff's band, for the file in the editor, how the diff is rendered and the review
// waiting to be sent.
func (r *Screen) diffMenu(in screen.Inputs, nest menu.NestedMenuBuilder) menu.Menu {
	reachable := !r.isArchived(in)
	_, onThread := r.diff.threadAtCursor()
	diffFocused := r.IsDiffFocused(in)

	entries := append(r.fileEntries(in),
		menu.Leaving(r.diffKeys().ExpandDesc(), r.deps.Keys.Diff.Expand, func(in screen.Inputs) screen.Request { return r.diffKeys().ToggleExpanded(in) }),
		menu.Leaving(keys.WrapDesc(in), r.deps.Keys.Diff.Wrap, func(in screen.Inputs) screen.Request { return r.diffKeys().ToggleWrap(in) }),
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
		entries = append(entries, menu.Leaving(fmt.Sprintf("end the range on snapshot %d", r.diff.newest.GetOrdinal()), r.deps.Keys.Review.RangeEnd, func(screen.Inputs) screen.Request {
			return r.endOnNewest()
		}))
	}

	entries = append(entries, menu.Leaving(r.reloadDesc(in), r.deps.Keys.Review.Reload, func(screen.Inputs) screen.Request { return r.catchUp() }))
	entries = append(entries, menu.Leaving(r.diff.agentThreadsDesc(), r.deps.Keys.Review.AgentThreads, r.toggleAgentThreads))

	if task, ok := in.Listing.Tasks[r.taskID]; ok {
		entries = append(entries, menu.Nested("git", r.deps.Keys.Menu.Git, nest.PullRequestGit(task)))
	}

	return menu.Menu{
		Title:   "review",
		Entries: entries,
	}
}

func (r *Screen) composeEntry(said string) menu.Entry {
	return menu.DrawnOff(menu.Leaving(r.composeDesc(), r.deps.Keys.Review.Compose, func(in screen.Inputs) screen.Request {
		return r.orRefuseWrite(in, r.compose)
	}), said)
}

func (r *Screen) answerEntry() menu.Entry {
	return menu.Leaving(r.diff.answerDesc(), r.deps.Keys.Review.Answer, func(screen.Inputs) screen.Request { return r.toggleAnswer() })
}

func (r *Screen) jumpEntry() menu.Entry {
	return menu.Leaving(r.diff.jumpDesc(), r.deps.Keys.Review.Jump, func(screen.Inputs) screen.Request { return r.toggleJump() })
}

// folderDesc says whether the folder key on the tree opens the folder or closes it.
func (r *Screen) folderDesc() string {
	folder, _ := r.readCurrentFolder()
	if r.closedFolders[folder.Path] {
		return "open folder"
	}

	return "close folder"
}
