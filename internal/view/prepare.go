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
		}))
	}

	columns := fitColumns(snapshot.Pane, snapshot.Dropped)
	if snapshot.Pane > 0 {
		width := NameColumns(snapshot.Pane, columns)
		for i := range lines {
			lines[i] = fitRow(lines[i], width)
		}
	}

	return List{Lines: lines, Columns: columns}
}

// fitColumns gives the columns up from the right as the pane narrows, reading the pane alone so
// a list does not change shape when a row gains a mark.
func fitColumns(pane int, dropped Columns) Columns {
	columns := Columns{Repo: !dropped.Repo, Review: !dropped.Review, Forge: !dropped.Forge}
	if pane <= 0 {
		return columns
	}

	// The repo column is never given up: a sidebar too narrow for the other two still says
	// whether the checkout under an agent is somewhere you can work.
	for _, give := range []*bool{&columns.Forge, &columns.Review} {
		if NameColumns(pane, columns) >= minNameColumns {
			return columns
		}

		*give = false
	}

	return columns
}

// prepareLineParams is one row and what the list around it says about that row.
type prepareLineParams struct {
	row         Row
	focused     bool
	caretHidden bool
}

// prepareLine resolves one row to the glyphs, tones and weight it is drawn in.
func prepareLine(params prepareLineParams) Line {
	row, focused := params.row, params.focused

	line := Line{
		Focused:  focused,
		Focus:    markCell(focused && !params.caretHidden, GlyphFocus, paint.ToneAccent),
		Mark:     textCell(row.Mark, paint.ToneMark),
		Repo:     RepoCell(row),
		Review:   ReviewCell(row),
		Forge:    forgeCell(row),
		Status:   StatusCell(row),
		Rule:     row.Kind == KindChild,
		Indent:   indentFor(row.Kind),
		Name:     row.Name,
		Matches:  row.Matches,
		Weight:   weightFor(row.Kind),
		Emphasis: emphasisFor(row, focused),
		Held:     HeldCell(row),
		Mail:     MailCell(row),
		Alert:    AlertCell(row),
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

// RepoCell is where the directory an agent sits in stands, greyed while nothing has happened to
// it and empty on a row that is not an agent's own.
func RepoCell(row Row) Cell {
	if row.Kind != KindAgent {
		return Cell{}
	}

	switch row.Repo {
	case RepoClean:
		return Cell{Glyph: GlyphRepo, Tone: paint.ToneMuted}
	case RepoRebasing:
		return Cell{Glyph: GlyphRepo, Tone: paint.ToneWarning}
	case RepoGone:
		return Cell{Glyph: GlyphBroken, Tone: paint.ToneDanger}
	}

	return Cell{Glyph: GlyphRepo, Tone: paint.ToneMuted}
}

// ReviewCell is where the local review of an agent's work stands.
func ReviewCell(row Row) Cell {
	if row.Kind != KindAgent {
		return Cell{}
	}

	switch row.Review {
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
func forgeCell(row Row) Cell {
	if row.Kind != KindAgent {
		return Cell{}
	}

	switch row.Forge {
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

// StatusCell is what the agent is doing this second, a bar rather than a glyph so it is noticed
// rather than decoded.
func StatusCell(row Row) Cell {
	switch row.Process {
	case ProcessNone:
		return Cell{}
	case ProcessStopped:
		return Cell{Glyph: GlyphStopped, Tone: paint.ToneMuted}
	case ProcessAlive:
	}

	switch row.Status {
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

// MailCell is the mark a row draws for mail its agent has not read.
func MailCell(row Row) Cell {
	return markCell(row.UnreadMail, GlyphMail, paint.ToneAccent)
}

// AlertCell is the mark a row draws for a script weave fired that failed.
func AlertCell(row Row) Cell {
	return markCell(row.FailedRun, GlyphAlert, paint.ToneDanger)
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

// indentFor is how far a row's name is set in from the gutter, an agent under its workflow and
// a child under its agent.
func indentFor(kind RowKind) int {
	if kind == KindWorkflow {
		return 0
	}

	return indentColumns
}

// fitRow gives a row's width up in order: the tally, then the name, and last the marks, which
// only a pane too narrow for the marks alone ever reaches.
func fitRow(line Line, width int) Line {
	// A tally counts rows already out of sight, which is worth less than the marks saying
	// somebody has to do something, so it is what gives way first.
	if nameColumns(line, width) <= 0 {
		line.Tally = 0
	}

	line.Name, line.Matches = clipName(line.Name, line.Matches, nameColumns(line, width))

	return shedMarks(line, width)
}

// shedMarks gives the marks after a name up, in the order of what a person can do without,
// where even a nameless row will not fit the pane.
func shedMarks(line Line, width int) Line {
	// The schedule is a standing fact about the row, the pause is one a person took themselves,
	// and mail goes before the failed run nobody else will mention.
	for _, shed := range []*Cell{&line.Schedule, &line.Held, &line.Mail, &line.Alert} {
		if nameColumns(line, width) >= 0 {
			return line
		}

		*shed = Cell{}
	}

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

// HeldCell is the pause a person took on an agent, in danger where that agent is working anyway.
func HeldCell(row Row) Cell {
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
