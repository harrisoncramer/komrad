package ui

import "weave/internal/ui/paint"

// region is a part of the panel that draws itself, and which the keys are either on or not.
type region int

const (
	// regionList is the list on the panel's own side, whichever screen is up.
	regionList region = iota
	// regionColumn is the block drawn beside that list.
	regionColumn
	// regionDetails is the record under the list.
	regionDetails
	// regionMessages is the reported message under that.
	regionMessages
	// regionThread is the review conversation, which stands in the sidebar in place of the list.
	regionThread
)

// holdsKeys reports whether a region is the one the keys are on.
func (m model) holdsKeys(area region) bool {
	switch area {
	case regionList:
		return m.isListFocused()
	case regionColumn:
		return m.isColumnFocused()
	case regionDetails:
		return m.isDetailsFocused()
	case regionMessages:
		return m.isMessagesFocused()
	case regionThread:
		return m.isReviewThreadFocused()
	}

	return false
}

// palette is what a region draws in: the panel's colours while it holds the keys, grey while
// they are anywhere else.
func (m model) palette(area region) paint.Palette {
	return paint.PaletteFor(m.holdsKeys(area))
}
