package view

import "weave/internal/ui/paint"

// Prepare turns a snapshot of the agents list into the view model a renderer draws.
func Prepare(snapshot Snapshot) List {
	lines := make([]Line, 0, len(snapshot.Rows))
	for _, row := range snapshot.Rows {
		lines = append(lines, prepareLine(row, isFocused(row, snapshot.Focused), snapshot.NameWidth))
	}

	return List{Lines: lines}
}

// prepareLine resolves one row to the glyphs, tones and weight it is drawn in.
func prepareLine(row Row, focused bool, nameWidth int) Line {
	line := Line{
		Focused:  focused,
		Focus:    markCell(focused, GlyphFocus, paint.ToneAccent),
		Mark:     textCell(row.Mark, paint.ToneMark),
		Repo:     repoCell(row.Repo),
		Review:   reviewCell(row.Review),
		Forge:    forgeCell(row.Forge),
		Status:   statusCell(row.Status, row.Dormant),
		Rule:     row.Kind == KindChild,
		Name:     clipName(row.Name, nameWidth),
		Weight:   weightFor(row.Kind),
		Emphasis: emphasisFor(row),
		Held:     markCell(row.Held, GlyphPaused, paint.ToneWarning),
		Mail:     markCell(row.UnreadMail, GlyphMail, paint.ToneAccent),
		Alert:    markCell(row.FailedRun, GlyphAlert, paint.ToneDanger),
		Tally:    row.HiddenCount,
		Schedule: markCell(row.Scheduled, GlyphSchedule, paint.ToneMuted),
		Selected: row.Selected,
	}

	if row.Dim {
		return greyed(line)
	}

	return line
}

// greyed takes the colour off every cell of a row, leaving the focus caret alone since a hidden
// row the keyboard is standing on still has to say so.
func greyed(line Line) Line {
	for _, cell := range []*Cell{&line.Mark, &line.Repo, &line.Review, &line.Forge, &line.Status, &line.Held, &line.Mail, &line.Alert, &line.Schedule} {
		cell.Tone = paint.ToneMuted
	}

	return line
}

// isFocused reports whether a row is the one the snapshot named, an unnamed row never being it.
func isFocused(row Row, focused string) bool {
	return focused != "" && row.Key == focused
}

// repoCell is where an agent's own checkout stands, always drawn so an untouched one reads as
// not yet rather than as missing.
func repoCell(repo Repo) Cell {
	switch repo {
	case RepoClean:
		return Cell{Glyph: GlyphRepo, Tone: paint.ToneMuted}
	case RepoRebasing:
		return Cell{Glyph: GlyphRepo, Tone: paint.ToneWarning}
	case RepoGone:
		return Cell{Glyph: GlyphBroken, Tone: paint.ToneDanger}
	}

	return Cell{Glyph: GlyphRepo, Tone: paint.ToneMuted}
}

// reviewCell is where the local review of an agent's work stands.
func reviewCell(review Review) Cell {
	switch review {
	case ReviewNone:
		return Cell{Glyph: GlyphReview, Tone: paint.ToneMuted}
	case ReviewOpen:
		return Cell{Glyph: GlyphReview, Tone: paint.ToneAccent}
	case ReviewApproved:
		return Cell{Glyph: GlyphApproved, Tone: paint.ToneSuccess}
	case ReviewChangesRequested:
		return Cell{Glyph: GlyphChanges, Tone: paint.ToneWarning}
	}

	return Cell{Glyph: GlyphReview, Tone: paint.ToneMuted}
}

// forgeCell is where the pull request opened from an agent's branch stands.
func forgeCell(forge Forge) Cell {
	switch forge {
	case ForgeNone:
		return Cell{Glyph: GlyphForge, Tone: paint.ToneMuted}
	case ForgeOpen:
		return Cell{Glyph: GlyphForge, Tone: paint.ToneAccent}
	case ForgeApproved:
		return Cell{Glyph: GlyphApproved, Tone: paint.ToneSuccess}
	case ForgeFailing:
		return Cell{Glyph: GlyphBroken, Tone: paint.ToneDanger}
	case ForgeMerged:
		return Cell{Glyph: GlyphMerged, Tone: paint.ToneMerged}
	}

	return Cell{Glyph: GlyphForge, Tone: paint.ToneMuted}
}

// statusCell is what the agent is doing this second, a bar rather than a glyph so it is noticed
// rather than decoded, and nothing at all where there is no process to report on.
func statusCell(status Status, dormant bool) Cell {
	if dormant {
		return Cell{}
	}

	switch status {
	case StatusIdle:
		return Cell{Glyph: GlyphQuiet, Tone: paint.ToneMuted}
	case StatusThinking:
		return Cell{Glyph: GlyphBusy, Tone: paint.ToneAccent}
	case StatusWaiting:
		return Cell{Glyph: GlyphBusy, Tone: paint.ToneWarning}
	case StatusBroken:
		return Cell{Glyph: GlyphBusy, Tone: paint.ToneDanger}
	}

	return Cell{Glyph: GlyphQuiet, Tone: paint.ToneMuted}
}

// markCell is the shape every optional mark takes: nothing at all when what it stands for is
// not true.
func markCell(show bool, glyph Glyph, tone paint.Tone) Cell {
	if !show {
		return Cell{}
	}

	return Cell{Glyph: glyph, Tone: tone}
}

// textCell is a cell holding a letter a person typed rather than a glyph the design names.
func textCell(text string, tone paint.Tone) Cell {
	if text == "" {
		return Cell{}
	}

	return Cell{Text: text, Tone: tone}
}

// weightFor is the lettering a row's place in the tree is carried by, spending no glyph on it.
func weightFor(kind RowKind) Weight {
	switch kind {
	case KindWorkflow:
		return WeightBold
	case KindAgent:
		return WeightRegular
	case KindChild:
		return WeightItalic
	}

	return WeightRegular
}

// clipName cuts a name to the column it was given, leaving it whole when the snapshot names no
// column at all.
func clipName(name string, width int) string {
	if width <= 0 {
		return name
	}

	return paint.Clip(name, width)
}

// emphasisFor is how brightly a name is drawn, greying beating brightening since a hidden row
// is out of the way whatever it has said.
func emphasisFor(row Row) Emphasis {
	switch {
	case row.Dim:
		return EmphasisDim
	case row.Unviewed:
		return EmphasisBright
	}

	return EmphasisNormal
}
