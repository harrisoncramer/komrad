package ui

import (
	"weave/config"

	tea "github.com/charmbracelet/bubbletea"
)

type screen int

const (
	screenAgents screen = iota
	screenSchedules
	screenArchive
	screenSearch
	screenReview
)

// screenSpec is everything the panel asks of whichever screen is up, resolved from the screen
// alone so that adding a screen is one arm here rather than an arm in every file that dispatches.
type screenSpec struct {
	// key takes a keypress the panel's own bindings did not claim.
	key func(model, tea.KeyMsg) (tea.Model, tea.Cmd)

	// view draws the screen into the panel's own pane.
	view func(model) string

	// stage puts the row the cursor is on into the stage pane, and is nil on a screen whose
	// stage is filled by something arriving later rather than drawn from what is already held.
	stage func(*model)

	// fromListing is whether the refresh's own results are what this screen draws, which is what
	// says a pass that lands should restage it.
	fromListing bool

	// reenter comes back to the screen with its cursor and stage put right, and is nil on a
	// screen nothing is ever drawn over.
	reenter func(model) (tea.Model, tea.Cmd)

	// typing reports whether something on the screen is taking every key and so standing the
	// panel's own bindings down, and is nil on a screen that never takes typing.
	typing func(model) bool

	details *detailsSpec
	filter  *filterSpec
}

// detailsSpec is the record a screen draws under its list, left nil by a screen that draws
// none, which is what says the details keys do nothing there.
type detailsSpec struct {
	// hovered is the record the cursor is standing on.
	hovered func(model) details

	// quit is the screen's own quit binding, since the details take the keys while they hold
	// the cursor and quitting has to keep working.
	quit func(config.Keybindings) []string
}

// filterSpec is how a filtered list keeps its cursor on the row it was on as the pattern changes
// under it, left nil by a screen that draws no filter.
type filterSpec struct {
	// hovered is the key of the row the cursor is on, read before the pattern changes.
	hovered func(model) string

	// place puts the cursor back on that row, or as near to where it was as the narrowed list
	// allows.
	place func(*model, string)
}

// spec carries no default arm, so the exhaustive check fails the build on a screen added
// without one.
func (s screen) spec() screenSpec {
	switch s {
	case screenAgents:
		return screenSpec{
			key:         model.updateAgents,
			view:        model.agentsView,
			stage:       (*model).stageSelection,
			fromListing: true,
			reenter:     model.returnToAgents,
			details: &detailsSpec{
				hovered: model.agentsDetails,
				quit:    func(keys config.Keybindings) []string { return keys.Agents.Quit },
			},
			filter: &filterSpec{
				hovered: model.hoveredAgentKey,
				place:   (*model).placeAgentCursor,
			},
		}
	case screenSchedules:
		return screenSpec{
			key:         model.updateSchedules,
			view:        model.schedulesView,
			stage:       (*model).stageSchedule,
			fromListing: true,
			reenter:     model.enterSchedules,
			details: &detailsSpec{
				hovered: model.schedulesDetails,
				quit:    func(keys config.Keybindings) []string { return keys.Schedules.Quit },
			},
			filter: &filterSpec{
				hovered: model.hoveredScheduleKey,
				place:   (*model).placeScheduleCursor,
			},
		}
	case screenArchive:
		return screenSpec{
			key:         model.updateArchive,
			view:        model.archiveView,
			stage:       (*model).stageArchive,
			fromListing: true,
			reenter:     model.enterArchive,
			details: &detailsSpec{
				hovered: model.archiveDetails,
				quit:    func(keys config.Keybindings) []string { return keys.Archive.Quit },
			},
			filter: &filterSpec{
				hovered: model.hoveredArchiveKey,
				place:   (*model).placeArchiveCursor,
			},
		}
	case screenSearch:
		return screenSpec{
			key:    model.updateSearch,
			view:   model.searchView,
			typing: func(m model) bool { return m.search.typing },
		}
	case screenReview:
		return screenSpec{
			key:    model.updateReview,
			view:   model.reviewView,
			stage:  (*model).stageReview,
			typing: func(m model) bool { return m.review.takesEveryKey() },
		}
	}

	// The switch carries every screen and no default, so this is what a screen added without an
	// arm compiles to until the exhaustive check refuses it.
	return screenSpec{}
}
