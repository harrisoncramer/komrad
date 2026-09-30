package view

import (
	"strconv"

	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/lib/xslice"
)

// Prepare turns a snapshot of the agents list into the view model a renderer draws.
func Prepare(snapshot Snapshot) List {
	columns := snapshot.columns()
	lines := xslice.Map(snapshot.Rows, func(row Row) Line {
		return prepareLine(prepareLineParams{
			row:        row,
			columns:    columns,
			focused:    isFocused(row, snapshot.Focused),
			caret:      snapshot.Caret,
			blinkedOut: snapshot.BlinkedOut,
		})
	})

	if snapshot.Pane > 0 {
		width := NameColumns(snapshot.Pane, columns)
		for i := range lines {
			lines[i] = fitRow(lines[i], width)
		}
	}

	return List{Lines: lines, Columns: columns, Glyphs: snapshot.Glyphs}
}

// prepareLineParams is one row and what the list around it says about that row.
type prepareLineParams struct {
	row        Row
	columns    Columns
	focused    bool
	caret      Caret
	blinkedOut bool
}

// prepareLine resolves one row to the glyphs, tones and weight it is drawn in.
func prepareLine(params prepareLineParams) Line {
	row, focused := params.row, params.focused

	return Line{
		Focused:  focused,
		Focus:    textCell(chromeIf(focused && params.caret == CaretDrawn, glyph.Focus), paint.ToneAccent),
		Columns:  columnCells(row, params.columns),
		Status:   blinkBar(StatusCell(row), row, params.blinkedOut),
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
		Schedule: markCell(row.Scheduled, glyph.Schedule, paint.ToneMuted),
		Comments: buildCommentCells(row),
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
		return Cell{Glyph: glyph.Worktree, Tone: paint.ToneMuted}
	case RepoRebasing:
		return Cell{Glyph: glyph.Worktree, Tone: paint.ToneWarning}
	case RepoGone:
		return Cell{Glyph: glyph.WorktreeGone, Tone: paint.ToneDanger}
	}

	return Cell{Glyph: glyph.Worktree, Tone: paint.ToneMuted}
}

// buildCommentCells lays out the counts of whoever has commented on an agent's work, reviewer
// agents then the person.
func buildCommentCells(row Row) []Cell {
	var cells []Cell
	for _, cell := range []Cell{reviewerCommentsCell(row), personCommentsCell(row)} {
		if !cell.IsBlank() {
			cells = append(cells, cell)
		}
	}

	return cells
}

// reviewerCommentsCell counts the comments reviewer agents left on an agent's work, open then
// resolved.
func reviewerCommentsCell(row Row) Cell {
	if !isOwn(row) || row.ReviewerComments == nil {
		return Cell{}
	}

	return countsCell(row.ReviewerComments, glyph.ReviewerComments, paint.ToneBright)
}

// personCommentsCell counts the comments the person left on an agent's work, the open count
// standing out while there is one, and a column after held for the dot any unread one earns.
func personCommentsCell(row Row) Cell {
	if !isOwn(row) || row.PersonComments == nil {
		return Cell{}
	}

	counts := countsCell(row.PersonComments, glyph.PersonComments, paint.ToneWarning)
	dot := Cell{Text: " "}
	if row.PersonComments.Unread {
		dot = Cell{Glyph: glyph.Unread, Tone: paint.ToneWarning}
	}

	return Cell{Parts: append(counts.Parts, dot)}
}

// countsCell is one side's open and resolved counts after its glyph.
func countsCell(comments *Comments, whose glyph.Glyph, openTone paint.Tone) Cell {
	tone := paint.ToneMuted
	if comments.Open > 0 {
		tone = openTone
	}

	return Cell{Parts: []Cell{
		{Glyph: whose, Tone: tone},
		{Text: " "},
		{Text: strconv.Itoa(comments.Open), Tone: tone},
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
		return Cell{Glyph: glyph.Open, Tone: paint.ToneMuted}
	case ForgeApproved:
		return Cell{Glyph: glyph.Approved, Tone: paint.ToneSuccess}
	case ForgeApprovedWaiting:
		return Cell{Glyph: glyph.Open, Tone: paint.ToneMuted}
	case ForgeChangesRequested:
		return Cell{Glyph: glyph.ChangesRequested, Tone: paint.ToneWarning}
	case ForgeChecksRunning:
		return Cell{Text: row.LoaderFrame, Tone: paint.ToneWarning}
	case ForgeDraft:
		return Cell{Glyph: glyph.Draft, Tone: paint.ToneMuted}
	case ForgeFailing:
		return Cell{Glyph: glyph.Failing, Tone: paint.ToneDanger}
	case ForgeConflicting:
		return Cell{Glyph: glyph.Conflicting, Tone: paint.ToneDanger}
	case ForgeMerged:
		return Cell{Glyph: glyph.Merged, Tone: paint.ToneMerged}
	case ForgeClosed:
		return Cell{Glyph: glyph.Closed, Tone: paint.ToneDanger}
	}

	return Cell{}
}

// StatusCell is what the agent is doing this second, a bar rather than a glyph so it is noticed
// rather than decoded.
func StatusCell(row Row) Cell {
	switch row.Status {
	case StatusIdle:
		return Cell{Text: glyph.Quiet, Tone: paint.ToneMuted}
	case StatusThinking:
		return Cell{Text: glyph.Busy, Tone: paint.ToneBright}
	case StatusWaiting:
		return Cell{Text: glyph.Busy, Tone: paint.ToneWarning}
	case StatusBroken:
		return Cell{Text: glyph.Busy, Tone: paint.ToneDanger}
	}

	return Cell{Text: glyph.Quiet, Tone: paint.ToneMuted}
}

// blinkBar thins a working agent's bar on the off half of the blink.
func blinkBar(cell Cell, row Row, out bool) Cell {
	if out && row.Status == StatusThinking && cell.Text == glyph.Busy {
		cell.Text = glyph.Quiet
	}

	return cell
}

// HiddenCell is the mark a row draws once a person has taken it out of the sidebar.
func HiddenCell(row Row) Cell {
	return markCell(row.Hidden, glyph.Hidden, paint.ToneMuted)
}

// MailCell is the mark a row draws for mail its agent has not read.
func MailCell(row Row) Cell {
	return markCell(row.UnreadMail, glyph.Mail, paint.ToneMuted)
}

// DocumentCell is the mark a row draws once its agent has written a document for a person to read.
func DocumentCell(row Row) Cell {
	return markCell(row.Documents, glyph.Document, paint.ToneMuted)
}

// AlertCell is the mark a row draws for a script komrad fired that failed.
func AlertCell(row Row) Cell {
	return markCell(row.FailedRun, glyph.FailedScript, paint.ToneDanger)
}

// markCell is the shape every optional mark takes: nothing at all when what it stands for is
// not true.
func markCell(show bool, mark glyph.Glyph, tone paint.Tone) Cell {
	if !show {
		return Cell{}
	}

	return Cell{Glyph: mark, Tone: tone}
}

// chromeIf is the chrome given when it is to be drawn, and nothing when it is not.
func chromeIf(show bool, chrome string) string {
	if !show {
		return ""
	}

	return chrome
}

// textCell is a cell holding text rather than a glyph a person may choose.
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

// fitRow gives a row's width up in order: the tally, then the comment counts, then the name, and
// last the marks, which only a pane too narrow for the marks alone ever reaches.
func fitRow(line Line, width int) Line {
	// A tally counts rows already out of sight, so it is what gives way first.
	if roomForName(line, width) <= 0 {
		line.Tally = 0
	}

	if roomForName(line, width) <= 0 {
		line.Comments = nil
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
		return Cell{Glyph: glyph.Paused, Tone: paint.ToneDanger}
	}

	return markCell(row.Held, glyph.Paused, paint.ToneMuted)
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
