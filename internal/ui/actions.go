package ui

import (
	"slices"
	"strings"

	"weave/config"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// actionID names one thing a screen does with a key, so the arm that answers it and the row that
// names it point at one entry rather than at two copies of a binding.
type actionID int

const (
	actionNone actionID = iota
	actionHelp
	actionMove
	actionJump
	actionColumnScroll
	actionDetails
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
	actionAgentOpenPR
	actionAgentReview
	actionAgentMail
	actionAgentChild
	actionAgentRename
	actionAgentToggleViewed
	actionAgentArchive
	actionArchiveSelected
	actionAgentHide
	actionAgentShowHidden
	actionAgentNew
	actionAgentSearch
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
)

// action is one arm of a screen's dispatch, the description living on the legend row instead
// because a row can name two actions and has room for one wording.
type action struct {
	id   actionID
	keys func(config.Keybindings) []string
	// when reports whether the press reaches this arm, and nil means always.
	when func(model) bool
	// run takes the press as well as the model, since a row that goes both ways shares one arm.
	run func(model, string) (tea.Model, tea.Cmd)
}

// legendRow is one line of the legend, naming the actions it stands for rather than repeating
// their keys, so a rebinding reaches it without being written down twice.
type legendRow struct {
	of   []actionID
	desc func(model) string
	// show overrides when the row is drawn, nil meaning whenever an action it names would answer,
	// the two differing where a key is taken only to refuse itself or drawn only to hold a height.
	show func(model) bool
	// label overrides how the keys are written, and nil spells one action's keys the way the rest
	// of the legend does and names a fused row by the first key of each half.
	label func(config.Keybindings) string
	// strip puts the row on the one-line legend drawn while the help is collapsed.
	strip bool
}

// actionSpec is a screen's keys, ordered twice: the table in the order the dispatch tries it,
// which is precedence, and the legend in the order a person reads it.
type actionSpec struct {
	table  []action
	legend []legendRow
}

// dispatch hands the press to the first arm that answers it, and reports whether one did.
func (s actionSpec) dispatch(m model, pressed string) (tea.Model, tea.Cmd, bool) {
	for _, a := range s.table {
		if a.when != nil && !a.when(m) {
			continue
		}

		if !isBound(pressed, a.keys(m.keys)) {
			continue
		}

		next, cmd := a.run(m, pressed)

		return next, cmd, true
	}

	return m, nil, false
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

// rowBinding is the row as bubbles wants it: every key of every action it names, under one label.
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

	// One action spells out its alternatives, having the row to itself, while a fused row names
	// each half by its first key.
	label := strings.Join(heads, "/")
	if len(row.of) == 1 {
		label = keyLabel(keys)
	}

	if row.label != nil {
		label = row.label(m.keys)
	}

	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(label, row.desc(m)))
}

func (s actionSpec) actionOf(id actionID) (action, bool) {
	i := slices.IndexFunc(s.table, func(a action) bool { return a.id == id })
	if i < 0 {
		return action{}, false
	}

	return s.table[i], true
}
