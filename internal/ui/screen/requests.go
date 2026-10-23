package screen

import tea "charm.land/bubbletea/v2"

// RunCmd is a command the root runs as it is, for a screen's own reads and timers.
type RunCmd tea.Cmd

// WaitForCursorRest runs the screen's OnCursorRest once the cursor stops moving.
type WaitForCursorRest struct{}

// RefreshStage puts the hovered agent's pane on the stage at once.
type RefreshStage struct{}

// ReshowOpenShell puts the panel's open shell back on the stage, where one is open.
type ReshowOpenShell struct{}

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

// MoveToMatch walks the cursor to the next match in Direction along the filtered list.
type MoveToMatch struct {
	Direction int
}

// ClearFilterRestoreCursor drops the filter, and puts the cursor back only when the filter was
// opened in the section that holds the keys now.
type ClearFilterRestoreCursor struct{}

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
	return PostMessage{
		MessageKind: kind,
		Summary:     summary,
		Detail:      summary,
	}
}

// RemoveMessage takes down the message a PostMessage was keyed by.
type RemoveMessage struct {
	MessageKey string
}

// Request marks RunCmd as a request.
func (RunCmd) Request() {}

// Request marks WaitForCursorRest as a request.
func (WaitForCursorRest) Request() {}

// Request marks RefreshStage as a request.
func (RefreshStage) Request() {}

// Request marks ReshowOpenShell as a request.
func (ReshowOpenShell) Request() {}

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

// Request marks MoveToMatch as a request.
func (MoveToMatch) Request() {}

// Request marks ClearFilterRestoreCursor as a request.
func (ClearFilterRestoreCursor) Request() {}

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
