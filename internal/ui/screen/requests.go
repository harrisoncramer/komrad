package screen

import tea "charm.land/bubbletea/v2"

// Cmd is a command the root runs as it is, for a screen's own reads and timers.
type Cmd tea.Cmd

// Settle stages what the cursor comes to rest on once it stops moving.
type Settle struct{}

// Restage puts the hovered agent's pane on the stage at once.
type Restage struct{}

// StageTerminal puts the screen's shell on the stage, or takes it down where the screen has none.
type StageTerminal struct{}

// OpenTerminal opens a shell beside the panel in a directory.
type OpenTerminal struct {
	Dir string
}

// Unstage takes the editor on the stage down and hands the keys back to the sidebar.
type Unstage struct{}

// SelectGuest hands the keyboard to the pane on the stage when it is the one named.
type SelectGuest struct {
	Pane string
}

// Reconcile puts the window back the way the screen up wants it.
type Reconcile struct{}

// PickView hands the keys to the view switcher.
type PickView struct{}

// Leave puts down what the screen up had open, then applies what Then returns where it is set.
type Leave struct {
	Then func() Request
}

// ResetFilter drops the filter without placing any cursor.
type ResetFilter struct{}

// BeginFilter opens the filter line on the list holding the keys.
type BeginFilter struct{}

// StepMatch walks the cursor Delta matches along the filtered list.
type StepMatch struct {
	Delta int
}

// ClearFilter drops the filter and puts the cursor back where the filter found it.
type ClearFilter struct{}

// Filter narrows the list up by a query, as if it were typed and applied.
type Filter struct {
	Query string
}

// FocusDoc hands the keys to the document beside the list.
type FocusDoc struct{}

// PlaceDoc puts the reader on a line of the document beside the list, with the window from Top.
type PlaceDoc struct {
	Line, Top int
}

// Wrap wraps a diff's lines wider than its pane, or cuts them off again.
type Wrap struct {
	On bool
}

// Inspect asks the server for one worktree directory's standing.
type Inspect struct {
	Dir string
}

// ReadLastScreen asks for the saved screen of the agent the screen up answers for.
type ReadLastScreen struct{}

// Copy puts text on the clipboard.
type Copy struct {
	Text string
}

// Kind is how a note reads.
type Kind int

const (
	Info Kind = iota
	Success
	Warning
	Error
)

// Note puts a message on the messages list.
type Note struct {
	Kind Kind
	// Key names the message so a later note or Unnote can find it, empty for one that stands alone.
	Key     string
	Summary string
	Detail  string
	Hint    string
}

// NewNote is a note that needs no more said than its summary.
func NewNote(kind Kind, summary string) Note {
	return Note{Kind: kind, Summary: summary, Detail: summary}
}

// Unnote takes down the message a note was keyed by.
type Unnote struct {
	Key string
}

// Request marks Cmd as a request.
func (Cmd) Request() {}

// Request marks Settle as a request.
func (Settle) Request() {}

// Request marks Restage as a request.
func (Restage) Request() {}

// Request marks StageTerminal as a request.
func (StageTerminal) Request() {}

// Request marks OpenTerminal as a request.
func (OpenTerminal) Request() {}

// Request marks Unstage as a request.
func (Unstage) Request() {}

// Request marks SelectGuest as a request.
func (SelectGuest) Request() {}

// Request marks Reconcile as a request.
func (Reconcile) Request() {}

// Request marks PickView as a request.
func (PickView) Request() {}

// Request marks Leave as a request.
func (Leave) Request() {}

// Request marks ResetFilter as a request.
func (ResetFilter) Request() {}

// Request marks BeginFilter as a request.
func (BeginFilter) Request() {}

// Request marks StepMatch as a request.
func (StepMatch) Request() {}

// Request marks ClearFilter as a request.
func (ClearFilter) Request() {}

// Request marks Filter as a request.
func (Filter) Request() {}

// Request marks FocusDoc as a request.
func (FocusDoc) Request() {}

// Request marks PlaceDoc as a request.
func (PlaceDoc) Request() {}

// Request marks Wrap as a request.
func (Wrap) Request() {}

// Request marks Inspect as a request.
func (Inspect) Request() {}

// Request marks ReadLastScreen as a request.
func (ReadLastScreen) Request() {}

// Request marks Copy as a request.
func (Copy) Request() {}

// Request marks Note as a request.
func (Note) Request() {}

// Request marks Unnote as a request.
func (Unnote) Request() {}
