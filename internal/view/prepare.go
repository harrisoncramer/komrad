package view

import "weave/internal/ui/paint"

const (
	// agentIndent pushes every row that hangs under a workflow one space across, leaving the
	// workflow itself opening at the gutter.
	agentIndent = 1
	// defaultNameWidth is the column a name is cut to when the snapshot names none.
	defaultNameWidth = 30
)

// Prepare turns a snapshot of the agents list into the view model a renderer draws.
func Prepare(snapshot Snapshot) List {
	width := snapshot.NameWidth
	if width <= 0 {
		width = defaultNameWidth
	}

	lines := make([]Line, 0, len(snapshot.Rows))
	for i, row := range snapshot.Rows {
		lines = append(lines, prepareLine(row, i == snapshot.Cursor, width))
	}

	return List{Lines: lines}
}

// prepareLine resolves one row to the glyphs, tones and weight it is drawn in.
func prepareLine(row Row, focused bool, nameWidth int) Line {
	line := Line{
		Focused:  focused,
		Mark:     row.Mark,
		Repo:     repoCell(row.Repo),
		Review:   reviewCell(row.Review),
		Forge:    forgeCell(row.Forge),
		Status:   statusCell(row.Status),
		Indent:   indentFor(row.Kind),
		Rule:     row.Kind == KindChild,
		Name:     paint.Clip(row.Name, nameWidth),
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

// greyed takes the colour off every cell of a row, which is what hidden and archived look like.
func greyed(line Line) Line {
	for _, cell := range []*Cell{&line.Repo, &line.Review, &line.Forge, &line.Status, &line.Held, &line.Mail, &line.Alert, &line.Schedule} {
		cell.Tone = paint.ToneMuted
	}

	return line
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
// rather than decoded.
func statusCell(status Status) Cell {
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

// indentFor is how far across a row's name opens.
func indentFor(kind RowKind) int {
	switch kind {
	case KindWorkflow:
		return 0
	case KindAgent, KindChild:
		return agentIndent
	}

	return agentIndent
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
