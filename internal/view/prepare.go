package view

import (
	"strconv"

	"komrad/internal/ui/paint"
	"komrad/lib/xslice"
)

// Prepare turns a snapshot of the agents list into the view model a renderer draws.
func Prepare(snapshot Snapshot) List {
	columns := snapshot.columns()
	lines := xslice.Map(snapshot.Rows, func(row Row) Line {
		return prepareLine(prepareLineParams{
			row:           row,
			columns:       columns,
			focused:       isFocused(row, snapshot.Focused),
			caret:         snapshot.Caret,
			barBlinkedOut: snapshot.BarBlinkedOut,
		})
	})

	if snapshot.Pane > 0 {
		width := NameColumns(snapshot.Pane, columns)
		for i := range lines {
			lines[i] = fitRow(lines[i], width)
		}
	}

	return List{Lines: lines, Columns: columns}
}

// prepareLineParams is one row and what the list around it says about that row.
type prepareLineParams struct {
	row           Row
	columns       Columns
	focused       bool
	caret         Caret
	barBlinkedOut bool
}

// prepareLine resolves one row to the glyphs, tones and weight it is drawn in.
func prepareLine(params prepareLineParams) Line {
	row, focused := params.row, params.focused

	return Line{
		Focused:  focused,
		Focus:    markCell(focused && params.caret == CaretDrawn, GlyphFocus, paint.ToneAccent),
		Mark:     textCell(row.Mark, paint.ToneMuted),
		Columns:  columnCells(row, params.columns),
		Status:   blinkBar(StatusCell(row), row, params.barBlinkedOut),
		Rule:     row.Kind == KindChild,
		Indent:   indentFor(row.Kind),
		Lead:     row.Lead,
		Name:     row.Name,
		Matches:  row.Matches,
		Weight:   weightFor(row.Kind),
		Emphasis: emphasisFor(row, focused && params.caret != CaretAbsent),
		Held:     HeldCell(row),
		Hidden:   HiddenCell(row),
		Document: DocumentCell(row),
		Mail:     MailCell(row),
		Alert:    AlertCell(row),
		Tally:    row.HiddenCount,
		Schedule: markCell(row.Scheduled, GlyphSchedule, paint.ToneMuted),
		Selected: row.Selected,
	}
}

// columnCells resolves a row's cell for each column the list draws.
func columnCells(row Row, columns Columns) []Cell {
	cells := make([]Cell, len(columns))
	for i, column := range columns {
		switch column.Kind {
		case ColumnRepo:
			cells[i] = RepoCell(row)
		case ColumnForge:
			cells[i] = ForgeCell(row)
		case ColumnReviewerComments:
			cells[i] = ReviewerCommentsCell(row)
		case ColumnPersonComments:
			cells[i] = PersonCommentsCell(row, column.Dotted)
		case ColumnText:
			if i < len(row.Cells) {
				cells[i] = row.Cells[i]
			}
		}
	}

	return cells
}

// isDim reports whether the process behind a row is gone.
func isDim(row Row) bool {
	return row.Process == ProcessStopped || row.Archived
}

// isFocused reports whether a row is the one the snapshot named, an unnamed row never being it.
func isFocused(row Row, focused string) bool {
	return focused != "" && row.Key == focused
}

// isOwn reports whether a row is an agent's own rather than a header's or a child's.
func isOwn(row Row) bool {
	return row.Kind == KindAgent || row.Kind == KindResult
}

// RepoCell is where the directory an agent sits in stands, greyed while nothing has happened to
// it and empty on a row that is not an agent's own.
func RepoCell(row Row) Cell {
	if !isOwn(row) {
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

// ReviewerCommentsCell counts the comments reviewer agents left on an agent's work, open then
// resolved.
func ReviewerCommentsCell(row Row) Cell {
	if !isOwn(row) || row.ReviewerComments == nil {
		return Cell{}
	}

	return countsCell(row.ReviewerComments, paint.ToneMuted)
}

// PersonCommentsCell counts the comments the person left on an agent's work, the open count
// standing out while there is one, and a dot after while any holds something unread.
func PersonCommentsCell(row Row, dotted bool) Cell {
	if !isOwn(row) || row.PersonComments == nil {
		return Cell{}
	}

	counts := countsCell(row.PersonComments, paint.ToneWarning)
	if !dotted {
		return counts
	}

	dot := Cell{Text: " "}
	if row.PersonComments.Unread {
		dot = Cell{Text: "·", Tone: paint.ToneWarning}
	}

	return Cell{Parts: append(counts.Parts, dot)}
}

// countsCell is a pair of counts, open then resolved, with the open count in the tone given
// while it is not zero.
func countsCell(comments *Comments, openTone paint.Tone) Cell {
	open := Cell{Text: strconv.Itoa(comments.Open), Tone: paint.ToneMuted}
	if comments.Open > 0 {
		open.Tone = openTone
	}

	return Cell{Parts: []Cell{
		open,
		{
			Text: "|" + strconv.Itoa(comments.Resolved),
			Tone: paint.ToneMuted,
		},
	}}
}

// ForgeCell is where the pull request opened from an agent's branch stands, blank while
// nothing is known or nothing was opened, and empty on a row that is not an agent's own.
func ForgeCell(row Row) Cell {
	if !isOwn(row) {
		return Cell{}
	}

	switch row.Forge {
	case ForgeUnread, ForgeNone, ForgeEnd:
		return Cell{}
	case ForgeOpen:
		return Cell{Glyph: GlyphForge, Tone: paint.ToneMuted}
	case ForgeApproved:
		return Cell{Glyph: GlyphApproved, Tone: paint.ToneSuccess}
	case ForgeApprovedWaiting:
		return Cell{Glyph: GlyphApproved, Tone: paint.ToneMuted}
	case ForgeChangesRequested:
		return Cell{Glyph: GlyphForge, Tone: paint.ToneWarning}
	case ForgeDraft:
		return Cell{Glyph: GlyphDraft, Tone: paint.ToneMuted}
	case ForgeFailing:
		return Cell{Glyph: GlyphFailing, Tone: paint.ToneDanger}
	case ForgeConflicting:
		return Cell{Glyph: GlyphConflicting, Tone: paint.ToneDanger}
	case ForgeMerged:
		return Cell{Glyph: GlyphMerged, Tone: paint.ToneMerged}
	case ForgeClosed:
		return Cell{Glyph: GlyphClosed, Tone: paint.ToneDanger}
	}

	return Cell{}
}

// StatusCell is what the agent is doing this second, a bar rather than a glyph so it is noticed
// rather than decoded.
func StatusCell(row Row) Cell {
	switch row.Status {
	case StatusIdle:
		return Cell{Glyph: GlyphQuiet, Tone: paint.ToneMuted}
	case StatusThinking:
		return Cell{Glyph: GlyphBusy, Tone: paint.ToneBright}
	case StatusWaiting:
		return Cell{Glyph: GlyphBusy, Tone: paint.ToneWarning}
	case StatusBroken:
		return Cell{Glyph: GlyphBusy, Tone: paint.ToneDanger}
	}

	return Cell{Glyph: GlyphQuiet, Tone: paint.ToneMuted}
}

// blinkBar thins a working agent's bar on the off half of the blink.
func blinkBar(cell Cell, row Row, out bool) Cell {
	if out && row.Status == StatusThinking && cell.Glyph == GlyphBusy {
		cell.Glyph = GlyphQuiet
	}

	return cell
}

// HiddenCell is the mark a row draws once a person has taken it out of the sidebar.
func HiddenCell(row Row) Cell {
	return markCell(row.Hidden, GlyphHidden, paint.ToneMuted)
}

// MailCell is the mark a row draws for mail its agent has not read.
func MailCell(row Row) Cell {
	return markCell(row.UnreadMail, GlyphMail, paint.ToneMuted)
}

// DocumentCell is the mark a row draws once its agent has written a document for a person to read.
func DocumentCell(row Row) Cell {
	return markCell(row.Documents, GlyphDocument, paint.ToneMuted)
}

// AlertCell is the mark a row draws for a script komrad fired that failed.
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
	case KindResult:
		return WeightRegular
	case KindHit:
		return WeightRegular
	}

	return WeightRegular
}

// indentFor is how far a row's name is set in from the gutter, an agent under its workflow and
// a child under its agent.
func indentFor(kind RowKind) int {
	if kind == KindWorkflow || kind == KindResult {
		return 0
	}

	return indentColumns
}

// fitRow gives a row's width up in order: the tally, then the name, and last the marks, which
// only a pane too narrow for the marks alone ever reaches.
func fitRow(line Line, width int) Line {
	// A tally counts rows already out of sight, so it is what gives way first.
	if roomForName(line, width) <= 0 {
		line.Tally = 0
	}

	line.Name, line.Matches = clipName(line.Name, line.Matches, roomForName(line, width))

	return shedMarks(line, width)
}

// shedMarks gives the marks after a name up, in the order of what a person can do without,
// where even a nameless row will not fit the pane.
func shedMarks(line Line, width int) Line {
	// The schedule and the documents are standing facts about the row, hiding and pausing are
	// things a person did themselves, mail goes before the output nobody has read, which asks
	// the person to look, and the failed run nobody else will mention goes last.
	for _, shed := range []*Cell{&line.Schedule, &line.Document, &line.Hidden, &line.Held, &line.Mail, &line.Alert} {
		if roomForName(line, width) >= 0 {
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

	return markCell(row.Held, GlyphPaused, paint.ToneMuted)
}

// emphasisFor is how brightly a name is drawn, the row under the cursor beating greying.
func emphasisFor(row Row, focused bool) Emphasis {
	switch {
	case focused:
		return EmphasisFocused
	case isDim(row):
		return EmphasisDim
	}

	return EmphasisNormal
}
