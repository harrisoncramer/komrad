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
	screenWorktrees
)

// opensInfoPane reports whether the keys that draw a pane beside the panel act on a screen,
// which a screen drawing the whole window itself says no to.
func (s screen) opensInfoPane() bool {
	switch s {
	case screenAgents, screenSchedules:
		return true
	case screenArchive, screenSearch, screenReview, screenWorktrees:
		return false
	}

	// The switch carries every screen and no default, so this is what a screen added without an
	// arm compiles to until the exhaustive check refuses it.
	return false
}

// screenSpec is everything the panel asks of whichever screen is up, resolved from the screen
// alone.
type screenSpec struct {
	// key takes a keypress the panel's own bindings did not claim.
	key func(model, tea.KeyMsg) (tea.Model, tea.Cmd)

	// view draws the screen into the panel's own pane.
	view func(model) string

	// stage puts the row the cursor is on into the stage pane, and is nil on a screen whose stage
	// is filled by something arriving later.
	stage func(*model)

	// fromListing is whether the refresh's own results are what this screen draws, so a pass that
	// lands should restage it.
	fromListing bool

	// needsArchive is whether the refresh has to list the archived agents too, a call the screens
	// that never name an archived agent do not pay.
	needsArchive bool

	// needsWorktrees is whether the refresh has to list the worktree directories too.
	needsWorktrees bool

	// reenter comes back to the screen with its cursor and stage put right, and is nil on a screen
	// nothing is ever drawn over.
	reenter func(model) (tea.Model, tea.Cmd)

	// typing reports whether something on the screen is taking every key and standing the panel's
	// own bindings down, and is nil on a screen that never takes typing.
	typing  func(model) bool
	details *detailsSpec
	filter  *filterSpec
	tabs    *tabsSpec

	// column is the block the screen draws beside its own list, left nil by a screen that draws
	// none, in which case the panel keeps the share of the window the frame gave it.
	column func() columnSpec
}

// detailsSpec is the record a screen draws under its list, left nil by a screen that draws none,
// in which case the details keys do nothing there.
type detailsSpec struct {
	// hovered is the record the cursor is standing on.
	hovered func(model) details

	// quit is the screen's own quit binding, which has to keep working while the details hold the
	// cursor and the keys.
	quit func(config.Keybindings) []string

	// refocus is called when the record takes the keys or gives them back, and is nil on a screen
	// that draws nothing saying where the keys are.
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

	// rows is every row on screen with the text a search reads, in the order they are drawn.
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
				fits:    model.doesAgentsFitDetails,
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
				fits:    model.doesSchedulesFitDetails,
			},
			filter: &filterSpec{
				hovered: model.hoveredScheduleKey,
				place:   (*model).placeScheduleCursor,
				rows:    model.scheduleFilterRows,
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
				fits:    model.doesArchiveFitDetails,
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
	case screenWorktrees:
		return screenSpec{
			key:            model.updateWorktrees,
			view:           model.worktreesView,
			stage:          (*model).stageWorktrees,
			fromListing:    true,
			needsArchive:   true,
			needsWorktrees: true,
			reenter:        model.enterWorktrees,
			details: &detailsSpec{
				hovered: model.worktreesDetails,
				quit:    func(keys config.Keybindings) []string { return keys.Worktrees.Quit },
				fits:    model.doesWorktreesFitDetails,
			},
			filter: &filterSpec{
				hovered: model.hoveredWorktreeKey,
				place:   (*model).placeWorktreeCursor,
				rows:    model.worktreeFilterRows,
			},
			tabs: &tabsSpec{
				strip: worktreesTabs(),
				state: func(m *model) *tabsState { return &m.worktrees.sort },
				fits:  model.doesWorktreesFitTabs,
			},
			column: worktreesColumn,
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
