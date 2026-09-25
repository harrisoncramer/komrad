package ui

import (
	"slices"
	"strings"

	"komrad/config"
	"komrad/lib/xslice"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// actionID names one thing a screen does with a key.
type actionID int

const (
	actionNone actionID = iota
	actionHelp
	actionMove
	actionJump
	actionNavPage
	actionColumnScroll
	actionDetails
	actionDetailsCopy
	actionMessages
	actionFilterOpen
	actionFilterClear
	actionFilterMatch
	actionLogsOpen
	actionLogsOlder
	actionTerminal
	actionQuit
	actionAgentsBack
	actionAgentOpen
	actionAgentReview
	actionGitMenu
	actionAgentChild
	actionAgentRename
	actionAgentArchive
	actionArchiveSelected
	actionAgentPause
	actionAgentReleaseSilently
	actionAgentHide
	actionAgentShowHidden
	actionAgentNew
	actionAgentRetry
	actionAgentSearch
	actionSearchOpen
	actionSearchEdit
	actionWorkflowNew
	actionWorkflowAdopt
	actionWorkflowRename
	actionWorkflowEdit
	actionWorkflowArchive
	actionSelectToggle
	actionSelectRange
	actionSelectClear
	actionMarkSet
	actionMarkJump
	actionSchedulesOpen
	actionArchiveOpen
	actionScheduleNew
	actionScheduleEdit
	actionScheduleToggle
	actionScheduleArchive
	actionScheduleBack
	actionArchiveRestore
	actionArchiveRestoreOpen
	actionArchiveDelete
	actionArchiveEmpty
	actionArchiveBack
	actionRestoreSelected
	actionDeleteSelected
	actionWorktreesOpen
	actionWorktreesBack
	actionWorktreesJumpToAgent
	actionWorktreesDelete
	actionWorktreesClear
	actionReviewBack
	actionReviewFile
	actionReviewThread
	actionReviewOpenThread
	actionReviewMatch
	actionReviewPan
	actionReviewPanReset
	actionReviewEdit
	actionReviewBase
	actionReviewReload
	actionReviewResolve
	actionReviewViewed
	actionReviewExpand
	actionReviewBaseForward
	actionReviewCompose
	actionReviewSend
	actionReviewFocusThread
)

type action struct {
	id   actionID
	keys func(config.Keybindings) []string
	when func(model) bool
	run  func(model, string) (tea.Model, tea.Cmd)
}

type legendRow struct {
	of    []actionID
	desc  func(model) string
	show  func(model) bool
	label func(config.Keybindings) string
	strip bool
}

// actionSpec is a screen's keys, ordered twice: the table in precedence order, the dispatch
// order, and the legend in reading order.
type actionSpec struct {
	table  []action
	legend []legendRow
}

// dispatch hands the press to the first arm that answers it, and gives the model back untouched
// when none does.
func (s actionSpec) dispatch(m model, pressed string) (tea.Model, tea.Cmd) {
	for _, a := range s.table {
		if a.when != nil && !a.when(m) {
			continue
		}

		if !isBound(pressed, a.keys(m.keys)) {
			continue
		}

		return a.run(m, pressed)
	}

	return m, nil
}

// help builds the legend from the same table the dispatch reads.
func (s actionSpec) help(m model) keyMap {
	var short, full []key.Binding

	for _, row := range s.legend {
		if !s.isRowShown(m, row) {
			continue
		}

		binding := s.rowBinding(m, row)
		full = append(full, binding)

		if row.strip {
			short = append(short, binding)
		}
	}

	return keyMap{short: short, full: full}
}

func (s actionSpec) isRowShown(m model, row legendRow) bool {
	if row.show != nil {
		return row.show(m)
	}

	return slices.ContainsFunc(row.of, func(id actionID) bool {
		a, ok := s.actionOf(id)

		return ok && (a.when == nil || a.when(m))
	})
}

// rowBinding converts the row into the format bubbleTea wants it: every key of every action it
// names, under one label.
func (s actionSpec) rowBinding(m model, row legendRow) key.Binding {
	var keys, heads []string

	for _, id := range row.of {
		a, ok := s.actionOf(id)
		if !ok {
			continue
		}

		bound := a.keys(m.keys)
		keys = append(keys, bound...)
		heads = append(heads, headLabel(bound))
	}

	label := strings.Join(heads, "/")
	if len(row.of) == 1 {
		label = keyLabel(keys)
	}

	if row.label != nil {
		label = row.label(m.keys)
	}

	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(label, row.desc(m)))
}

// dispatchArms is the table as the keybinding check walks it.
func (s actionSpec) dispatchArms(keys config.Keybindings) []dispatchArm {
	return xslice.Map(s.table, func(a action) dispatchArm {
		return dispatchArm{
			describe: "an action of the screen's own",
			keys:     a.keys(keys),
			guarded:  a.when != nil,
		}
	})
}

func (s actionSpec) actionOf(id actionID) (action, bool) {
	i := slices.IndexFunc(s.table, func(a action) bool { return a.id == id })
	if i < 0 {
		return action{}, false
	}

	return s.table[i], true
}
