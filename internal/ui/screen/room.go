package screen

import (
	"time"

	"komrad/internal/ui/column"
	"komrad/internal/ui/list"
	"komrad/internal/view"
)

// Room is the window the panel has this frame and who holds its keys.
type Room struct {
	// Width is the column the panel renders its own list, record and legend in.
	Width int
	// IsListHalfOfSplit is whether the screen is rendered as the list half of a split the root has
	// already chosen.
	IsListHalfOfSplit bool
	// PaneHeight is the rows of the panel's pane.
	PaneHeight int
	// WindowSplit is how the window divides between the list and the block beside it, over the whole
	// height.
	WindowSplit column.Split
	// BlockAreaSplit is the window split with whatever stands over the block taken off its rows.
	BlockAreaSplit column.Split
	// OwnBlockSplit is the column a screen's own block is rendered across, refused while a form opens
	// outside the popup or the messages hold the keys.
	OwnBlockSplit column.Split
	// IsListFocused is whether the keys are the list's.
	IsListFocused bool
	// AreMessagesFocused is whether the messages hold the keys.
	AreMessagesFocused bool
	// IsFormInPopup is whether a form stands in the review's popup and holds the keys.
	IsFormInPopup bool
	// ListCaret is how the list's arrow stands.
	ListCaret view.Caret
	// DocumentReader is where the reader stands in the document beside the list.
	DocumentReader DocReader
	// IsDarkBackground is whether the terminal's background is dark.
	IsDarkBackground bool
	// IsDiffWrapped is whether a diff wraps its long lines.
	IsDiffWrapped bool
	// IsHelpExpanded is whether the legend is shown in full.
	IsHelpExpanded bool
	// SpinnerFrame is the loading spinner's frame.
	SpinnerFrame    string
	Now             time.Time
	ShouldMarkZones bool
	Pattern         list.Pattern
}

// DocReader is where the reader stands in the document beside the list.
type DocReader struct {
	CursorLine, TopLine int
	// HoldsKeys is whether the document holds the keys.
	HoldsKeys   bool
	Held        bool
	VisibleRows int
}
