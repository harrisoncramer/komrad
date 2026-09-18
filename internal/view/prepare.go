package view

import "weave/internal/ui/paint"

// Prepare turns a snapshot of the agents list into the view model a renderer draws.
func Prepare(snapshot Snapshot) List {
	lines := make([]Line, 0, len(snapshot.Rows))
	for _, row := range snapshot.Rows {
		lines = append(lines, prepareLine(prepareLineParams{
			row:         row,
			focused:     isFocused(row, snapshot.Focused),
			caretHidden: snapshot.CaretHidden,
			nameWidth:   snapshot.NameWidth,
		}))
	}

	return List{Lines: lines}
}

// prepareLineParams is one row and what the list around it says about that row.
type prepareLineParams struct {
	row         Row
	focused     bool
	caretHidden bool
	nameWidth   int
}

// prepareLine resolves one row to the glyphs, tones and weight it is drawn in.
func prepareLine(params prepareLineParams) Line {
	row, focused := params.row, params.focused

	line := Line{
		Focused:  focused,
		Focus:    markCell(focused && !params.caretHidden, GlyphFocus, paint.ToneAccent),
		Mark:     textCell(row.Mark, paint.ToneMark),
		Repo:     repoCell(row.Repo),
		Review:   reviewCell(row.Review),
		Forge:    forgeCell(row.Forge),
		Status:   statusCell(row.Status, row.Dormant),
		Rule:     row.Kind == KindChild,
		Name:     row.Name,
		Matches:  row.Matches,
		Weight:   weightFor(row.Kind),
		Emphasis: emphasisFor(row, focused),
		Held:     heldCell(row),
		Mail:     markCell(row.UnreadMail, GlyphMail, paint.ToneAccent),
		Alert:    markCell(row.FailedRun, GlyphAlert, paint.ToneDanger),
		Tally:    row.HiddenCount,
		Schedule: markCell(row.Scheduled, GlyphSchedule, paint.ToneMuted),
		Selected: row.Selected,
	}

	if params.nameWidth > 0 {
		line = fitName(line, row, params.nameWidth)
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
	case ReviewCommented:
		return Cell{Glyph: GlyphReview, Tone: paint.ToneMuted}
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

// fitName gives a name what the row has left, dropping the tally first where there is not even a
// column for it.
func fitName(line Line, row Row, width int) Line {
	// A tally counts rows already out of sight, which is worth less than the marks saying
	// somebody has to do something, so it is what gives way.
	if nameColumns(line, width) <= 0 {
		line.Tally = 0
	}

	line.Name, line.Matches = clipName(row.Name, row.Matches, nameColumns(line, width))

	return line
}

// clipName cuts a name to the columns it was left, along with the matches that survive the cut,
// and to nothing at all where it was left none.
func clipName(name string, matches []Span, width int) (string, []Span) {
	if width <= 0 {
		return "", nil
	}

	if len([]rune(name)) <= width {
		return name, matches
	}

	kept := len(string([]rune(name)[:width-1]))

	return paint.Clip(name, width), clipSpans(matches, kept)
}

// clipSpans drops the matches the cut took and shortens the one it cut through.
func clipSpans(matches []Span, kept int) []Span {
	clipped := make([]Span, 0, len(matches))
	for _, match := range matches {
		if match.Start >= kept {
			continue
		}

		clipped = append(clipped, Span{Start: match.Start, End: min(match.End, kept)})
	}

	if len(clipped) == 0 {
		return nil
	}

	return clipped
}

// heldCell is the pause a person took on an agent, in danger where that agent is working anyway.
func heldCell(row Row) Cell {
	if row.Held && row.HeldWorking {
		return Cell{Glyph: GlyphPaused, Tone: paint.ToneDanger}
	}

	return markCell(row.Held, GlyphPaused, paint.ToneWarning)
}

// emphasisFor is how brightly a name is drawn, the row under the cursor beating greying and
// greying beating brightening.
func emphasisFor(row Row, focused bool) Emphasis {
	switch {
	case focused:
		return EmphasisFocused
	case row.Dim:
		return EmphasisDim
	case row.Unviewed:
		return EmphasisBright
	}

	return EmphasisNormal
}
