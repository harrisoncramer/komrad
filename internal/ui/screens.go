package ui

import (
	"weave/config"
	"weave/internal/ui/layout"
	"weave/internal/ui/panel"
	agentv1 "weave/proto/gen/agent/v1"

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

// opensInfoPane reports whether the keys that draw a pane beside the panel act on a screen.
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

// screenModel is what the panel asks of whichever screen is up.
type screenModel interface {
	// update takes a message the panel's own bindings did not claim.
	update(m model, msg tea.Msg) (tea.Model, tea.Cmd)

	// view draws the screen into the panel's own pane.
	view(m model) string

	// needs is what the refresh has to fetch for the screen.
	needs() screenNeeds

	// open lands on the screen with its cursor on the target where the target is on screen,
	// and its stage put right.
	open(m model, target screenTarget) (tea.Model, tea.Cmd)

	// takesEveryKey reports whether something on the screen is taking every key.
	takesEveryKey(m model) bool
}

// screenTarget is the row a screen is asked to land on, read by each screen for the fields it
// understands.
type screenTarget struct {
	agentID string
}

// screenNeeds is what the refresh fetches for a screen.
type screenNeeds struct {
	// fromListing is whether the refresh's own results are what the screen draws.
	fromListing bool

	// archive is whether the refresh has to list the archived agents too.
	archive bool

	// worktrees is whether the refresh has to list the worktree directories too.
	worktrees bool
}

// detailed is a screen that draws a record under its list.
type detailed interface {
	// details is the record the cursor is standing on.
	details(m model) details

	// detailsQuit is the screen's own quit binding, kept working while the record holds the keys.
	detailsQuit(keys config.Keybindings) []string

	// detailsFit is whether the screen has the room to draw the record at all.
	detailsFit(m model) bool

	// detailsRoom is the rows the screen has under its header and the legend it is drawing.
	detailsRoom(m model) int
}

// filtered is a screen that draws a search under its list and keeps its cursor on the row it
// was on as the pattern changes.
type filtered interface {
	// hoveredKey is the key of the row the cursor is on, read before the pattern changes.
	hoveredKey(m model) string

	// placeAt puts the cursor back on that row, or as near to where it was as the narrowed list
	// allows.
	placeAt(m *model, key string)

	// filterRows is every row on screen with the text a search reads, in the order they are
	// drawn.
	filterRows(m model) []filterRow
}

// filterRow is one row a search can land on: the key that names it and the text it matches by.
type filterRow struct {
	key  string
	name string
}

// tabbed is a screen that draws a strip of tabs over its list.
type tabbed interface {
	tabs() tabStrip

	// tabsState is where the screen keeps the tab chosen and whether the strip holds the keys.
	tabsState(m *model) *tabsState

	// tabsFit is whether the screen has the room to draw the strip.
	tabsFit(m model) bool
}

// columned is a screen that draws a block beside its own list.
type columned interface {
	column() columnSpec
}

// staged is a screen that puts the row the cursor is on into the stage pane.
type staged interface {
	stage(m *model)
}

// active is the screen that is up.
func (m model) active() screenModel {
	return m.screen.adapter()
}

// adapter is the screen behind the enum, every one of them still a set of methods on the root.
func (s screen) adapter() screenModel {
	switch s {
	case screenAgents:
		return agentsAdapter{}
	case screenSchedules:
		return schedulesAdapter{}
	case screenArchive:
		return archiveAdapter{}
	case screenSearch:
		return searchAdapter{}
	case screenReview:
		return reviewAdapter{}
	case screenWorktrees:
		return worktreesAdapter{}
	}

	// The switch carries every screen and no default, so this is what a screen added without an
	// arm compiles to until the exhaustive check refuses it.
	return nil
}

// host is what a screen asks of the root.
type host interface {
	writeCmd(summary string, write func(api) error) tea.Cmd
	rowWriteCmd(summary string, write func(api) (written, error)) tea.Cmd
	openingWriteCmd(summary string, write func(api) (*agentv1.Agent, error)) tea.Cmd
	queuedCmd(summary string, queued message, write func(api) error) tea.Cmd
	report(summary string, err error)
	reportBrief(summary, detail string, err error)
	openForm(f formState) tea.Cmd
	enter(next screen, target screenTarget) tea.Cmd
	refreshCmd() tea.Cmd
	placeColumn(owner screen, row string)
	stageInfo()
	waitToStage() tea.Cmd
	hide() tea.Cmd
	client() api
	repo() ReviewGit

	// An agent stopped for a pass is let go with the files the reader opened over it.
	noteEdited(agentID string, touched []string)
	releaseCmd(summary string, agent panel.Agent, said string) tea.Cmd

	// The stage is the root's, and a screen may put an editor on it.
	stageEditor(content layout.Content) string
	unstageEditor()
	isPaneAlive(pane string) bool

	// The pane on the stage is the agents cursor's, wherever that cursor is asked for.
	stageSelection()
	selectGuest(pane string)

	// A jump onto a row a filter took off the screen has the filter widened into a search.
	widenFilter()

	// The keys every list shares are the root's to answer.
	takeTopPress(pressed string) bool
	isTopHeld() bool
	stepIntoDetails() tea.Cmd
	beginFilter()
	clearFilter()
	jumpToFilterMatch(pressed string) tea.Cmd
}

var _ host = (*model)(nil)

// client is the service as a screen reads it.
func (m *model) client() api {
	return m.api
}

// repo is the git a screen reads a worktree through.
func (m *model) repo() ReviewGit {
	return m.git
}

// stageEditor puts an editor on the stage with the keys in it, and answers the pane it was
// given, empty where the split was refused.
func (m *model) stageEditor(content layout.Content) string {
	m.frame.Show(layout.RegionStage, content)

	pane := m.frame.Pane(layout.RegionStage)
	if pane != "" {
		m.tmux.SelectPane(pane)
	}

	return pane
}

// unstageEditor takes the editor down and puts the keys back on the sidebar.
func (m *model) unstageEditor() {
	m.frame.Show(layout.RegionStage, newBlankContent())
	m.tmux.SelectPane(m.frame.Pane(layout.RegionSidebar))
}

func (m *model) isPaneAlive(pane string) bool {
	return m.tmux.IsPaneAlive(pane)
}

// selectGuest hands the keyboard to the pane on the stage when it is the one named.
func (m *model) selectGuest(pane string) {
	if guest := m.frame.GuestPane(); guest != "" && guest == pane {
		m.tmux.SelectPane(pane)
	}
}

// widenFilter turns a standing filter into a search, putting every row back on screen.
func (m *model) widenFilter() {
	m.filter.mode = filterModeSearch
}

// enter leaves whichever screen is up for the next, landing it on the target.
func (m *model) enter(next screen, target screenTarget) tea.Cmd {
	entered, cmd := next.adapter().open(*m, target)
	if landed, ok := entered.(model); ok {
		*m = landed
	}

	return cmd
}

// panelShared is what the root hands a screen on every call, and nothing the screen may write.
type panelShared struct {
	listing listing
	keys    config.Keybindings
	width   int
	height  int

	// pane is the column the screen draws its own list, record and legend in.
	pane   int
	chrome chromeState

	// inspections is what the service has answered for each worktree directory.
	inspections map[string]worktreeInspection

	// pullRequests is what the service has answered for each agent's branch.
	pullRequests pullRequestsState

	// runLogsDir is the directory the run logs are written under.
	runLogsDir string

	// editor is the command a file is opened in.
	editor string

	// isDark is the terminal's background.
	isDark bool

	// draw is the frame the root draws a screen's own list, record and legend in.
	draw drawer
}

// chromeState is the details, filter, column, messages, help and form the root draws over any
// screen.
type chromeState struct {
	details      detailsState
	filter       filterState
	column       columnState
	messages     messagesState
	form         formState
	helpExpanded bool
}

// shared is the root as a screen is allowed to see it.
func (m *model) shared() panelShared {
	s := m.unmeasured()
	s.pane = m.paneWidth()

	return s
}

// unmeasured is the root as a screen sees it before the column has been measured.
func (m *model) unmeasured() panelShared {
	return panelShared{
		listing: m.listing,
		keys:    m.keys,
		width:   m.width,
		height:  m.height,
		chrome: chromeState{
			details:      m.details,
			filter:       m.filter,
			column:       m.column,
			messages:     m.messages,
			form:         m.form,
			helpExpanded: m.helpExpanded,
		},
		inspections:  m.inspections,
		pullRequests: m.pullRequests,
		runLogsDir:   m.settings.RunLogsDir,
		editor:       m.settings.Editor,
		isDark:       m.isDark,
		draw:         m,
	}
}
