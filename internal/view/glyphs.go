package view

// Drawn is one glyph the list draws, with the width the layout counts it as.
type Drawn struct {
	Name    string
	Text    string
	Columns int
}

// Drawings is every glyph the list draws and the width its layout assumes of each.
func Drawings() []Drawn {
	drawings := make([]Drawn, 0, len(glyphNames))
	for _, named := range glyphNames {
		drawings = append(drawings, Drawn{
			Name:    named.name,
			Text:    Spell(named.glyph),
			Columns: plainColumns,
		})
	}

	return drawings
}

// glyphNames is what each glyph is called in a report, since a Glyph is a number everywhere else.
var glyphNames = []struct {
	glyph Glyph
	name  string
}{
	{GlyphRepo, "repo"},
	{GlyphReview, "review"},
	{GlyphForge, "forge"},
	{GlyphApproved, "approved"},
	{GlyphBroken, "broken"},
	{GlyphChanges, "changes"},
	{GlyphMerged, "merged"},
	{GlyphAlert, "alert"},
	{GlyphMail, "mail"},
	{GlyphPaused, "paused"},
	{GlyphSchedule, "schedule"},
	{GlyphBusy, "busy"},
	{GlyphQuiet, "quiet"},
	{GlyphStopped, "stopped"},
	{GlyphChild, "child"},
	{GlyphFocus, "focus"},
}
