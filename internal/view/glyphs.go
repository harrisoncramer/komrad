package view

import "komrad/lib/xslice"

// Drawn is one glyph the list draws, with the width the layout counts it as.
type Drawn struct {
	Name    string
	Text    string
	Columns int
}

// Drawings is every glyph the list draws and the width its layout assumes of each.
func Drawings() []Drawn {
	return xslice.Map(glyphNames, func(named namedGlyph) Drawn {
		return Drawn{
			Name:    named.name,
			Text:    Spell(named.glyph),
			Columns: plainColumns,
		}
	})
}

// namedGlyph is a glyph and what a report calls it.
type namedGlyph struct {
	glyph Glyph
	name  string
}

// glyphNames is what each glyph is called in a report.
var glyphNames = []namedGlyph{
	{GlyphRepo, "repo"},
	{GlyphForge, "forge"},
	{GlyphApproved, "approved"},
	{GlyphBroken, "broken"},
	{GlyphMerged, "merged"},
	{GlyphDraft, "draft"},
	{GlyphClosed, "closed"},
	{GlyphAlert, "alert"},
	{GlyphMail, "mail"},
	{GlyphPaused, "paused"},
	{GlyphHidden, "hidden"},
	{GlyphSchedule, "schedule"},
	{GlyphWorkflow, "workflow"},
	{GlyphArchive, "archive"},
	{GlyphSearch, "search"},
	{GlyphBusy, "busy"},
	{GlyphQuiet, "quiet"},
	{GlyphChild, "child"},
	{GlyphFocus, "focus"},
	{GlyphFailing, "failing"},
	{GlyphConflicting, "conflicting"},
	{GlyphDocument, "document"},
}
