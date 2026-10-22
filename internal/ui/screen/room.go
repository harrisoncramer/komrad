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
	// Height is the rows of the panel's pane.
	Height int
	// Geometry is how the window divides between the list and the block beside it, over the whole
	// height.
	Geometry column.Split
	// Column is the geometry with whatever stands over the block taken off its rows.
	Column column.Split
	// Split is the column a screen's own block is rendered across, refused while a form opens
	// outside the popup or the messages hold the keys.
	Split column.Split
	// ListFocused is whether the keys are the list's.
	ListFocused bool
	// MessagesFocused is whether the messages hold the keys.
	MessagesFocused bool
	// PopupForm is whether a form stands in the review's popup and holds the keys.
	PopupForm bool
	// Caret is how the list's arrow stands.
	Caret view.Caret
	// Doc is where the reader stands in the document beside the list.
	Doc DocReader
	// Dark is whether the terminal's background is dark.
	Dark bool
	// Wrap is whether a diff wraps its long lines.
	Wrap bool
	// HelpExpanded is whether the legend is shown in full.
	HelpExpanded bool
	// Spinner is the loading spinner's frame.
	Spinner string
	Now     time.Time
	Marks   bool
	Pattern list.Pattern
}

// DocReader is where the reader stands in the document beside the list.
type DocReader struct {
	Line, Top int
	// Focused is whether the document holds the keys.
	Focused bool
	Held    bool
	Window  int
}
