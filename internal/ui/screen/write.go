package screen

import (
	"komrad/internal/ui/layout"
	"komrad/internal/ui/panel"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
)

// Write sends a change to the server off the panel's loop, and hands what it answered back to
// the screen that asked.
type Write struct {
	// Doing is what the panel says the write is doing while it is in flight, empty for a write
	// it neither counts nor holds a form up for.
	Doing string
	// FailureSummary is what a failure is reported under, empty when the screen reports its own.
	FailureSummary string
	// UnstageFirst is the agents whose panes come off the stage before the write is sent.
	UnstageFirst []panel.Agent
	Send         func() WriteResult
}

// WriteResult is what a write answered with.
type WriteResult struct {
	Err error
	// WaitFor is what the panel must show before the write finishes.
	WaitFor WaitFor
	// CreatedRow is the row the write made, which the panel lands on once it is shown.
	CreatedRow *CreatedRow
	// SuccessNotice is what to say once the write has landed.
	SuccessNotice *Notice
	// Killed says the write took down the agents it unstaged.
	Killed bool
	// FollowUpReplacesForm says FollowUp opens the form that takes the held one's place.
	FollowUpReplacesForm bool
	// FollowUp is the asking screen's own message, routed once the write finishes.
	FollowUp tea.Msg
}

// CreatedRow is the row a write made.
type CreatedRow struct {
	RowKey  string
	Agent   string
	Pane    string
	Harness string
	// Focus says the pane is handed the keyboard once it is shown.
	Focus bool
	// Archived says the row is in the archive rather than the agents list.
	Archived bool
}

// Notice is what the panel says once a write has landed.
type Notice struct {
	Summary string
	Detail  string
}

// WaitFor is what the panel must show before a write finishes.
type WaitFor struct {
	RowsShown []Row
	RowsGone  []Row
	// Pane is the pane whose harness is to have rendered itself, empty when there is none to wait on.
	Pane    string
	Harness string
}

// List is which of the listing's lists a row is looked for in.
type List int

const (
	ListAgents List = iota
	ListArchivedAgents
	ListWorkflows
	ListArchivedWorkflows
	ListSchedules
	ListArchivedSchedules
	ListWorktrees
)

// Row is one row of the listing, by the list it sits in and its id.
type Row struct {
	List List
	ID   string
}

// Stage puts a pane on the stage with the keys in it, and hands the pane's id back to the screen
// that asked, empty where the split was refused.
type Stage struct {
	Content layout.Content
	Staged  func(pane string) tea.Msg
}

// Request marks Write as a request.
func (Write) Request() {}

// Request marks Stage as a request.
func (Stage) Request() {}

// RowsIn names the given ids as rows of one list.
func RowsIn(list List, ids ...string) []Row {
	return xslice.Map(ids, func(id string) Row { return Row{List: list, ID: id} })
}
