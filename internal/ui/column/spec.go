package column

import (
	"time"

	"komrad/internal/ui/panel"
)

// Spec is the block a screen renders beside its own list, with its funcs bound to what the screen
// was handed when it answered.
type Spec struct {
	// Floor is the narrowest the block is worth rendering at, below which the sidebar keeps the
	// whole window.
	Floor int

	// DocRows renders the block's rows at a width, and is nil on a screen that renders and scrolls
	// its block itself.
	DocRows func(width int) []string

	// Read reports whether the block is a document the keys can be handed to, and is nil on a
	// screen whose block never is.
	Read            func() bool
	SearchDebounce  time.Duration
	SearchMinLength int

	// Said is the agent whose conversation a said search shows in place of the block, and is nil on a
	// screen whose block never gives way to one.
	Said func() string

	// Holds reports whether the screen is rendering its own block at all, and is nil on a screen
	// that always is.
	Holds func() bool

	// Rows is the row budget the block is actually rendered in, and is nil on a screen with nothing
	// standing over it.
	Rows func(split Split) int

	// Beside is the agent the screen keeps on the stage beside its block, and is nil on a screen
	// that keeps none.
	Beside func() (panel.AgentRow, bool)
}

// IsHeld reports whether the screen is rendering its own block rather than handing its columns to
// another process.
func (s Spec) IsHeld() bool {
	return s.Holds == nil || s.Holds()
}

// IsAlwaysRead is the Read of a block that is a document whatever the screen is showing.
func IsAlwaysRead() bool {
	return true
}
