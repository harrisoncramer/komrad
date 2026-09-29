package glyph

import "komrad/lib/xslice"

// The chrome is what the layout is drawn with, and it is fixed.
const (
	// Busy is the status bar of an agent that is working or waiting on a person.
	Busy = "┃"
	// Quiet is the status bar of an agent with nothing going on.
	Quiet = "│"
	// Child is the corner a child hangs off its parent by.
	Child = "└"
	// Focus is the arrow on the row the keyboard stands on.
	Focus = "›"
	// Divider is the line between two columns.
	Divider = "│"
	// Rule is the line across the width of a pane.
	Rule = "─"
	// TeeLeft is where a rule leaves a divider on its left.
	TeeLeft = "├"
	// TeeRight is where a rule meets a divider on its right.
	TeeRight = "┤"
	// Cross is where a rule crosses a divider.
	Cross = "┼"
	// TextCursor is the cursor at the end of text being typed.
	TextCursor = "▏"
	// Separator sits between two things said on the same line.
	Separator = "·"
	// Crumb sits between two steps of the trail down to the row or page the cursor is on.
	Crumb = "›"
	// Up points at what lies above, or ahead.
	Up = "↑"
	// Down points at what lies below, or behind.
	Down = "↓"
	// Right points at where something has gone.
	Right = "→"
)

// Logo is the mark beside komrad's name at the top of the panel, outside the approved set.
const Logo = "\U000F050C"

// Chrome is every piece of chrome and what a report calls it.
func Chrome() []Named {
	return []Named{
		{Name: "busy", Text: Busy},
		{Name: "quiet", Text: Quiet},
		{Name: "child", Text: Child},
		{Name: "focus", Text: Focus},
		{Name: "divider", Text: Divider},
		{Name: "rule", Text: Rule},
		{Name: "tee left", Text: TeeLeft},
		{Name: "tee right", Text: TeeRight},
		{Name: "cross", Text: Cross},
		{Name: "text cursor", Text: TextCursor},
		{Name: "separator", Text: Separator},
		{Name: "crumb", Text: Crumb},
		{Name: "up", Text: Up},
		{Name: "down", Text: Down},
		{Name: "right", Text: Right},
	}
}

// Named is a character the panel draws and what a report calls it.
type Named struct {
	Name string
	Text string
}

// Drawn is every character the panel draws with the set given, each glyph by its key and then
// the chrome.
func (s Set) Drawn() []Named {
	drawn := xslice.Map(All(), func(g Glyph) Named {
		return Named{Name: g.Key(), Text: s.Spell(g)}
	})

	return append(drawn, Chrome()...)
}
