package tabs

import "slices"

const (
	Down Direction = iota
	Up
)

// NoStop is a focused or a chosen stop that stands on none of the row.
const NoStop = -1

const FlatLabel = "flat"

type Direction int

// Stops is a row of labels the tab keys walk, the focused one rendered as the cursor and the chosen
// one as the choice in force.
type Stops struct {
	Labels      []string
	FocusedStop int
	ChosenStop  int
	MarkStop    func(int, string) string
}

type Option struct {
	ID    string
	Label string
}

type Strip struct {
	Title    string
	Legend   string
	Options  []Option
	Fallback string
	// GroupedLabel is the stop's label while the rows sit under their headings.
	GroupedLabel string
}

type State struct {
	ChosenColumn string
	Direction    Direction
	IsFocused    bool
	// FocusedColumn is the column the keys act on, the chosen one while empty.
	FocusedColumn string
	// IsOnGroupStop says the focus is on the stop after the columns.
	IsOnGroupStop  bool
	AreHeadingsOff bool
}

// StepStop is the stop delta along from at in a row of count, wrapping at both ends.
func StepStop(at, delta, count int) int {
	return (at + delta + count) % count
}

// Flip is the other way the arrow points.
func (d Direction) Flip() Direction {
	if d == Up {
		return Down
	}

	return Up
}

// Leave takes the focus off the strip and back to the chosen column.
func (s *State) Leave() {
	s.IsFocused, s.FocusedColumn, s.IsOnGroupStop = false, "", false
}

// IndexOf is the column the state chose, or the fallback where the strip holds no such column.
func (s Strip) IndexOf(state State) int {
	at := slices.IndexFunc(s.Options, func(option Option) bool { return option.ID == state.ChosenColumn })
	if at < 0 {
		at = max(slices.IndexFunc(s.Options, func(option Option) bool { return option.ID == s.Fallback }), 0)
	}

	return at
}

// CountStops is the stops on the strip, the group stop included.
func (s Strip) CountStops() int {
	if s.GroupedLabel != "" {
		return len(s.Options) + 1
	}

	return len(s.Options)
}

// Step moves the focus along the strip, wrapping at both ends, and chooses nothing.
func (s Strip) Step(state *State, delta int) {
	at := StepStop(s.focusIndex(*state), delta, s.CountStops())
	state.IsOnGroupStop = at == len(s.Options)
	if state.IsOnGroupStop {
		state.FocusedColumn = ""

		return
	}
	state.FocusedColumn = s.Options[at].ID
}

// Choose sorts by the focused column in the given direction.
func (s Strip) Choose(state *State, direction Direction) {
	state.ChosenColumn, state.Direction = s.Options[s.focusIndex(*state)].ID, direction
	state.FocusedColumn = ""
}

// focusIndex is the stop the focus stands on, the group stop being one past the columns.
func (s Strip) focusIndex(state State) int {
	if state.IsOnGroupStop {
		return len(s.Options)
	}

	at := slices.IndexFunc(s.Options, func(option Option) bool { return option.ID == state.FocusedColumn })
	if at < 0 {
		return s.IndexOf(state)
	}

	return at
}
