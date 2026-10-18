package glyph

import "komrad/lib/xslice"

// The chrome is what the layout is drawn with, and it is fixed.
const (
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
	// Step marks the header of a step in a log.
	Step = "▸"
	// Span sits between where a range starts and where it ends.
	Span = "▸"
)

// Logo is the mark beside komrad's name at the top of the panel, outside the approved set.
const Logo = "\U000F050C"

// Chrome is every piece of chrome and what a report calls it.
func Chrome() []Named {
	return []Named{
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
		{Name: "step", Text: Step},
		{Name: "span", Text: Span},
	}
}

// Named is a character the panel draws and what a report calls it.
type Named struct {
	Name string
	Text string
}

// Drawing is one glyph the list draws, with the width the layout counts it as.
type Drawing struct {
	Name    string
	Text    string
	Columns int
}

// Drawings is every character the list draws with the set.
func (s Set) Drawings() []Drawing {
	drawn := xslice.Map(All(), func(g Glyph) Named {
		return Named{Name: g.Key(), Text: s.Spell(g)}
	})

	return xslice.Map(append(drawn, Chrome()...), func(named Named) Drawing {
		return Drawing{
			Name:    named.Name,
			Text:    named.Text,
			Columns: Columns,
		}
	})
}
