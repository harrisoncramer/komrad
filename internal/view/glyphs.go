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
	{GlyphUnverified, "unverified"},
	{GlyphForge, "forge"},
	{GlyphApproved, "approved"},
	{GlyphBroken, "broken"},
	{GlyphCheckbox, "checkbox"},
	{GlyphBell, "bell"},
	{GlyphHourglass, "hourglass"},
	{GlyphMerged, "merged"},
	{GlyphDraft, "draft"},
	{GlyphClosed, "closed"},
	{GlyphAlert, "alert"},
	{GlyphMail, "mail"},
	{GlyphPaused, "paused"},
	{GlyphHidden, "hidden"},
	{GlyphSchedule, "schedule"},
	{GlyphBusy, "busy"},
	{GlyphQuiet, "quiet"},
	{GlyphChild, "child"},
	{GlyphFocus, "focus"},
}
