package screen

import tea "charm.land/bubbletea/v2"

// RunCmd is a command the root runs as it is, for a screen's own reads and timers.
type RunCmd tea.Cmd

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

// UnstageEditor takes the editor on the stage down and hands the keys back to the sidebar.
type UnstageEditor struct{}

// FocusStagedPane hands the keyboard to the pane on the stage when it is the one named.
type FocusStagedPane struct {
	Pane string
}

// RestoreWindowLayout puts the window back the way the screen up wants it.
type RestoreWindowLayout struct{}

// FocusViewSwitcher hands the keys to the view switcher.
type FocusViewSwitcher struct{}

// Leave puts down what the screen up had open, then applies what AfterClose returns where it is set.
type Leave struct {
	AfterClose func() Request
}

// DropFilterLeaveCursor drops the filter without placing any cursor.
type DropFilterLeaveCursor struct{}

// BeginFilter opens the filter line on the list holding the keys.
type BeginFilter struct{}

// StepMatch walks the cursor Delta matches along the filtered list.
type StepMatch struct {
	Delta int
}

// ClearFilter drops the filter and puts the cursor back where the filter found it.
type ClearFilter struct{}

// ApplyFilter narrows the list up by a query, as if it were typed and applied.
type ApplyFilter struct {
	Query string
}

// FocusDocument hands the keys to the document beside the list.
type FocusDocument struct{}

// PlaceDocumentReader puts the reader on a line of the document beside the list, with the window
// from TopLine.
type PlaceDocumentReader struct {
	CursorLine, TopLine int
}

// SetDiffWrap wraps a diff's lines wider than its pane, or cuts them off again.
type SetDiffWrap struct {
	ShouldWrap bool
}

// InspectWorktree asks the server for one worktree directory's standing.
type InspectWorktree struct {
	Dir string
}

// ReadSavedScreen asks for the saved screen of the agent the screen up answers for.
type ReadSavedScreen struct{}

// Copy puts text on the clipboard.
type Copy struct {
	Text string
}

// Kind is how a message reads.
type Kind int

const (
	Info Kind = iota
	Success
	Warning
	Error
)

// PostMessage puts a message on the messages list.
type PostMessage struct {
	MessageKind Kind
	// MessageKey names the message so a later PostMessage or RemoveMessage can find it, empty for
	// one that stands alone.
	MessageKey string
	Summary    string
	Detail     string
	Hint       string
}

// NewPostMessage is a message that needs no more said than its summary.
func NewPostMessage(kind Kind, summary string) PostMessage {
	return PostMessage{MessageKind: kind, Summary: summary, Detail: summary}
}

// RemoveMessage takes down the message a PostMessage was keyed by.
type RemoveMessage struct {
	MessageKey string
}

// Request marks RunCmd as a request.
func (RunCmd) Request() {}

// Request marks Settle as a request.
func (Settle) Request() {}

// Request marks Restage as a request.
func (Restage) Request() {}

// Request marks StageTerminal as a request.
func (StageTerminal) Request() {}

// Request marks OpenTerminal as a request.
func (OpenTerminal) Request() {}

// Request marks UnstageEditor as a request.
func (UnstageEditor) Request() {}

// Request marks FocusStagedPane as a request.
func (FocusStagedPane) Request() {}

// Request marks RestoreWindowLayout as a request.
func (RestoreWindowLayout) Request() {}

// Request marks FocusViewSwitcher as a request.
func (FocusViewSwitcher) Request() {}

// Request marks Leave as a request.
func (Leave) Request() {}

// Request marks DropFilterLeaveCursor as a request.
func (DropFilterLeaveCursor) Request() {}

// Request marks BeginFilter as a request.
func (BeginFilter) Request() {}

// Request marks StepMatch as a request.
func (StepMatch) Request() {}

// Request marks ClearFilter as a request.
func (ClearFilter) Request() {}

// Request marks ApplyFilter as a request.
func (ApplyFilter) Request() {}

// Request marks FocusDocument as a request.
func (FocusDocument) Request() {}

// Request marks PlaceDocumentReader as a request.
func (PlaceDocumentReader) Request() {}

// Request marks SetDiffWrap as a request.
func (SetDiffWrap) Request() {}

// Request marks InspectWorktree as a request.
func (InspectWorktree) Request() {}

// Request marks ReadSavedScreen as a request.
func (ReadSavedScreen) Request() {}

// Request marks Copy as a request.
func (Copy) Request() {}

// Request marks PostMessage as a request.
func (PostMessage) Request() {}

// Request marks RemoveMessage as a request.
func (RemoveMessage) Request() {}
