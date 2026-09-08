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
	screenMail
)

// opensInfoPane reports whether the keys that draw a pane beside the panel act on a screen, which
// the screens drawing the whole window themselves say no to rather than hand it back to a pane
// drawn over what they came to show.
func (s screen) opensInfoPane() bool {
	switch s {
	case screenAgents, screenSchedules, screenMail:
		return true
	case screenArchive, screenSearch, screenReview:
		return false
	}

	// The switch carries every screen and no default, so this is what a screen added without an
	// arm compiles to until the exhaustive check refuses it.
	return false
}

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

	// needsArchive is whether the refresh has to list the archived agents too, which costs a
	// call the screens that never name an archived agent do not pay.
	needsArchive bool

	// reenter comes back to the screen with its cursor and stage put right, and is nil on a
	// screen nothing is ever drawn over.
	reenter func(model) (tea.Model, tea.Cmd)

	// typing reports whether something on the screen is taking every key and so standing the
	// panel's own bindings down, and is nil on a screen that never takes typing.
	typing  func(model) bool
	details *detailsSpec
	filter  *filterSpec

	// column is the block the screen draws beside its own list, left nil by a screen that draws
	// none, which is what says the panel keeps the share of the window the frame gave it.
	column func() columnSpec
}

// detailsSpec is the record a screen draws under its list, left nil by a screen that draws
// none, which is what says the details keys do nothing there.
type detailsSpec struct {
	// hovered is the record the cursor is standing on.
	hovered func(model) details

	// quit is the screen's own quit binding, which has to keep working while the details hold the
	// cursor and the keys.
	quit func(config.Keybindings) []string

	// refocus is called when the record takes the keys or gives them back, and is nil on a screen
	// that draws nothing saying where the keys are, which the review does: the row the reader is
	// on is drawn in a pane of its own and has to be repainted to dim.
	refocus func(*model)

	// fits is whether the screen has the room to draw the record at all, nil on a screen that
	// always has it, and what stops the keys being taken by a record the pane cannot show.
	fits func(model) bool
}

// filterSpec is how a filtered list keeps its cursor on the row it was on as the pattern changes
// under it, left nil by a screen that draws no filter.
type filterSpec struct {
	// hovered is the key of the row the cursor is on, read before the pattern changes.
	hovered func(model) string

	// place puts the cursor back on that row, or as near to where it was as the narrowed list
	// allows.
	place func(*model, string)

	// rows is every row on screen with the text a search reads, in the order they are drawn, and
	// is left unset by a screen whose search narrows instead, where there is nothing to walk.
	rows func(model) []filterRow
}

// filterRow is one row a search can land on: the key that names it and the text it matches by.
type filterRow struct {
	key  string
	name string
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
				rows:    model.agentFilterRows,
			},
			column: agentsColumn,
		}
	case screenSchedules:
		return screenSpec{
			key:         model.updateSchedules,
			view:        model.schedulesView,
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
			column: schedulesColumn,
		}
	case screenArchive:
		return screenSpec{
			key:          model.updateArchive,
			view:         model.archiveView,
			stage:        (*model).stageArchive,
			fromListing:  true,
			needsArchive: true,
			reenter:      model.enterArchive,
			details: &detailsSpec{
				hovered: model.archiveDetails,
				quit:    func(keys config.Keybindings) []string { return keys.Archive.Quit },
			},
			filter: &filterSpec{
				hovered: model.hoveredArchiveKey,
				place:   (*model).placeArchiveCursor,
				rows:    model.archiveFilterRows,
			},
			column: archiveColumn,
		}
	case screenSearch:
		return screenSpec{
			key:          model.updateSearch,
			view:         model.searchView,
			needsArchive: true,
			typing:       func(m model) bool { return m.search.typing },
			column:       searchColumn,
		}
	case screenMail:
		return screenSpec{
			key:          model.updateMail,
			view:         model.mailView,
			stage:        (*model).stageMail,
			needsArchive: true,
			details: &detailsSpec{
				hovered: model.mailDetails,
				quit:    func(keys config.Keybindings) []string { return keys.Mail.Quit },
			},
			filter: &filterSpec{
				hovered: model.hoveredMailKey,
				place:   (*model).placeMailCursor,
			},
			column: mailColumn,
		}
	case screenReview:
		return screenSpec{
			key:    model.updateReview,
			view:   model.reviewView,
			typing: func(m model) bool { return m.review.doesTakeEveryKey() },
			details: &detailsSpec{
				hovered: model.reviewDetails,
				quit:    func(keys config.Keybindings) []string { return keys.Review.Quit },
				fits:    model.doesReviewFitDetails,
			},
			column: reviewColumn,
		}
	}

	// The switch carries every screen and no default, so this is what a screen added without an
	// arm compiles to until the exhaustive check refuses it.
	return screenSpec{}
}
