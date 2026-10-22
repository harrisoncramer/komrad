package keys

import (
	"slices"
	"strings"

	"komrad/config"
	"komrad/internal/ui/legend"
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"

	"charm.land/bubbles/v2/key"
)

// ID names one thing a screen does with a key.
type ID int

// The actions every screen shares.
const (
	_ ID = iota
	Move
	Jump
	NavPage
	ColumnScroll
	ColumnBack
	Details
	DetailsCopy
	FilterOpen
	FilterClear
	FilterMatch
	Quit
	SelectToggle
	selectRange
	selectClear
	// Own is the first ID a package declares of its own, free to match another package's since no
	// table mixes the two.
	Own
)

// NotInMenu is the reason on an action arm that has not moved into the menu yet.
const NotInMenu = "not yet moved into the menu"

// Action is one arm of a screen's dispatch, which moves through nav or acts as a declared direct
// exception.
type Action[S any] struct {
	ID     ID
	Keys   func(config.Keybindings) []string
	When   func(S) bool
	Nav    func(S, string) screen.Request
	Direct *direct[S]
}

// direct is an arm that acts straight off its key rather than from the menu, and why.
type direct[S any] struct {
	Reason string
	Run    func(S, string) screen.Request
}

// Directly is an arm's direct action, with the reason it skips the menu.
func Directly[S any](reason string, run func(S, string) screen.Request) *direct[S] {
	return &direct[S]{Reason: reason, Run: run}
}

// LegendRow is one row of a screen's legend.
type LegendRow[S any] struct {
	Of    []ID
	Desc  func(S) string
	Show  func(S) bool
	Label func(config.Keybindings) string
	Strip bool
}

// Spec is a screen's keys, ordered twice: the table in precedence order, the dispatch order, and
// the legend in reading order.
type Spec[S any] struct {
	Table  []Action[S]
	Legend []LegendRow[S]
}

// Arm is one arm of the order a press is offered to, described for the error it would be named
// in and guarded where it answers only some of the presses that reach it.
type Arm struct {
	Describe string
	Keys     []string
	Guarded  bool
}

// Table is a screen's table as the keybinding check walks it.
type Table interface {
	Arms(bindings config.Keybindings) []Arm
}

// Dispatch hands the press to the first arm that answers it.
func (s Spec[S]) Dispatch(state S, bindings config.Keybindings, pressed string) screen.Request {
	for _, a := range s.Table {
		if a.When != nil && !a.When(state) {
			continue
		}

		if !IsBound(pressed, a.Keys(bindings)) {
			continue
		}

		if a.Direct != nil {
			return a.Direct.Run(state, pressed)
		}

		return a.Nav(state, pressed)
	}

	return nil
}

// Help builds the short and the full legend from the same table the dispatch reads.
func (s Spec[S]) Help(state S, bindings config.Keybindings) legend.Help {
	var help legend.Help
	for _, row := range s.Legend {
		if !s.isRowShown(state, row) {
			continue
		}

		binding := s.rowBinding(state, bindings, row)
		help.Full = append(help.Full, binding)

		if row.Strip {
			help.Short = append(help.Short, binding)
		}
	}

	return help
}

func (s Spec[S]) isRowShown(state S, row LegendRow[S]) bool {
	if row.Show != nil {
		return row.Show(state)
	}

	return slices.ContainsFunc(s.Table, func(a Action[S]) bool {
		return slices.Contains(row.Of, a.ID) && (a.When == nil || a.When(state))
	})
}

// rowBinding converts the row into the format bubbleTea wants it: every key of every action it
// names, under one label.
func (s Spec[S]) rowBinding(state S, bindings config.Keybindings, row LegendRow[S]) key.Binding {
	var keys, heads []string

	for _, id := range row.Of {
		a, ok := s.answeringActionOf(state, id)
		if !ok {
			continue
		}

		bound := a.Keys(bindings)
		keys = append(keys, bound...)
		heads = append(heads, HeadLabel(bound))
	}

	label := strings.Join(heads, "/")
	if len(row.Of) == 1 {
		label = Label(keys)
	}

	if row.Label != nil {
		label = row.Label(bindings)
	}

	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(label, row.Desc(state)))
}

// answeringActionOf is the first arm with the id that answers now, or the first arm with it.
func (s Spec[S]) answeringActionOf(state S, id ID) (Action[S], bool) {
	i := slices.IndexFunc(s.Table, func(a Action[S]) bool {
		return a.ID == id && (a.When == nil || a.When(state))
	})
	if i < 0 {
		return s.actionOf(id)
	}

	return s.Table[i], true
}

func (s Spec[S]) actionOf(id ID) (Action[S], bool) {
	i := slices.IndexFunc(s.Table, func(a Action[S]) bool { return a.ID == id })
	if i < 0 {
		return Action[S]{}, false
	}

	return s.Table[i], true
}

// Arms is the table as the keybinding check walks it.
func (s Spec[S]) Arms(bindings config.Keybindings) []Arm {
	return xslice.Map(s.Table, func(a Action[S]) Arm {
		return Arm{
			Describe: "an action of the screen's own",
			Keys:     a.Keys(bindings),
			Guarded:  a.When != nil,
		}
	})
}
