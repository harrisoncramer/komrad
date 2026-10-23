package view

import (
	"strconv"
	"strings"

	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/lib/xslice"
)

// Prepare turns a snapshot of the agents list into the view model a renderer renders.
func Prepare(snapshot Snapshot) List {
	columns := snapshot.columns()
	lines := xslice.Map(snapshot.Rows, func(row Row) line {
		return prepareLine(prepareLineParams{
			row:     row,
			columns: columns,
			focused: isFocused(row, snapshot.Focused),
			caret:   snapshot.Caret,
		})
	})

	if snapshot.Pane > 0 {
		width := nameColumns(snapshot.Pane, columns)
		for i := range lines {
			lines[i] = fitRow(lines[i], width)
		}
	}

	return List{
		Lines:   lines,
		Columns: columns,
		Glyphs:  snapshot.Glyphs,
	}
}

// prepareLineParams is one row and what the list around it says about that row.
type prepareLineParams struct {
	row     Row
	columns Columns
	focused bool
	caret   Caret
}

// prepareLine resolves one row to the glyphs, tones and weight it renders in.
func prepareLine(params prepareLineParams) line {
	row, focused := params.row, params.focused
	caret := focused && params.caret == CaretRendered

	return line{
		Focused:   focused,
		Focus:     textCell(chromeIf(caret, glyph.Focus), paint.TonePrimary),
		Columns:   columnCells(row, params.columns),
		Branch:    branchFor(row.Tree),
		Rule:      (row.Kind == KindChild || row.Nested) && row.Tree == nil,
		Indent:    indentFor(row),
		Lead:      row.Lead,
		Role:      markCell(row.Reviewer, glyph.Reviewer, paint.ToneMuted),
		Name:      row.Name,
		Matches:   row.Matches,
		Editing:   row.Editing,
		Behind:    row.Behind,
		Weight:    weightFor(row.Kind),
		Emphasis:  emphasisFor(row, caret),
		Working:   workingCell(row),
		Hidden:    HiddenCell(row),
		Document:  DocumentCell(row),
		AutoMerge: AutoMergeCell(row),
		Mail:      MailCell(row),
		Alert:     AlertCell(row),
		Tool:      toolCell(row),
		Tally:     row.HiddenCount,
		Schedule:  markCell(row.Scheduled, glyph.Schedule, paint.ToneMuted),
		Workflow:  row.Workflow,
		Tail:      row.Tail,
		Comments:  buildCommentCells(row),
		Selected:  row.Selected,
		Divider:   row.Kind == KindDivider,
	}
}

// columnCells resolves a row's cell for each column the list renders.
func columnCells(row Row, columns Columns) []Cell {
	cells := make([]Cell, len(columns))
	for i, column := range columns {
		switch column.Kind {
		case columnRepo:
			cells[i] = RepoCell(row)
		case columnForge:
			cells[i] = ForgeCell(row)
		case ColumnText:
			if i < len(row.Cells) {
				cells[i] = row.Cells[i]
			}
		}
	}

	return cells
}

// isDim reports whether the process behind a row is gone or the keyboard cannot reach it.
func isDim(row Row) bool {
	return row.Process == ProcessStopped || row.Archived || row.Faded
}

// isFocused reports whether a row is the one the snapshot named, an unnamed row never being it.
func isFocused(row Row, focused string) bool {
	return focused != "" && row.Key == focused
}

// isOwn reports whether a row is an agent's own rather than a header's, a child's or one under
// the task whose row speaks for it.
func isOwn(row Row) bool {
	return (row.Kind == KindAgent || row.Kind == KindResult) && !row.Nested
}

// carriesWork reports whether a row's gutter and comments say how the work behind it stands,
// which an agent's own row and a task's do.
func carriesWork(row Row) bool {
	return isOwn(row) || row.Kind == KindTask
}

// RepoCell is where the directory an agent sits in stands, greyed while nothing has happened to
// it, and empty on a row that carries no work, one in its workflow's own directory, or one
// whose worktree was deleted.
func RepoCell(row Row) Cell {
	if !carriesWork(row) || row.InWorkflowDir {
		return Cell{}
	}

	switch row.Repo {
	case RepoClean:
		return Cell{Glyph: glyph.Worktree, Tone: paint.ToneMuted}
	case RepoRebasing:
		return Cell{Glyph: glyph.Worktree, Tone: paint.ToneWarning}
	case RepoGone:
		return Cell{Glyph: glyph.WorktreeGone, Tone: paint.ToneDanger}
	case RepoDeleted:
		return Cell{}
	}

	return Cell{Glyph: glyph.Worktree, Tone: paint.ToneMuted}
}

// buildCommentCells lays out the counts outstanding on an agent's work against the right edge.
func buildCommentCells(row Row) []Cell {
	if !carriesWork(row) {
		return nil
	}

	reviewerOpen, personOpen := 0, 0
	if row.ReviewerComments != nil {
		reviewerOpen = row.ReviewerComments.Open
	}
	if row.PersonComments != nil {
		personOpen = row.PersonComments.Open
	}

	var cells []Cell
	for _, cell := range []Cell{
		countCell(glyph.ReviewerComments, reviewerOpen, paint.ToneMuted),
		countCell(glyph.PersonComments, personOpen, paint.ToneMuted),
		UnreadCell(row),
	} {
		if !cell.IsBlank() {
			cells = append(cells, cell)
		}
	}

	return cells
}

// UnreadCell counts the person's comments holding a reply they have not read, empty on a row that
// carries no work.
func UnreadCell(row Row) Cell {
	if !carriesWork(row) || row.PersonComments == nil {
		return Cell{}
	}

	return countCell(glyph.Unread, row.PersonComments.Unread, paint.ToneWarning)
}

// countCell is one count after its glyph, and nothing while the count is zero.
func countCell(mark glyph.Glyph, count int, tone paint.Tone) Cell {
	if count == 0 {
		return Cell{}
	}

	return Cell{Parts: []Cell{
		{Glyph: mark, Tone: tone},
		{Text: " "},
		{Text: strconv.Itoa(count), Tone: tone},
	}}
}

// ForgeCell is where the pull request opened from an agent's branch stands, blank while
// nothing is known or nothing was opened, and empty on a row that carries no work.
func ForgeCell(row Row) Cell {
	if !carriesWork(row) {
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
	case ForgeRereviewRequested:
		return Cell{Glyph: glyph.ChangesRequested, Tone: paint.ToneMuted}
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

// workingCell is the loader turning after the name of an agent at work, greyed so it is there to
// glance at rather than to draw the eye.
func workingCell(row Row) Cell {
	if row.Process != ProcessAlive || row.Status != StatusThinking || row.LoaderFrame == "" {
		return Cell{}
	}

	return Cell{Text: row.LoaderFrame, Tone: paint.ToneMuted}
}

// toolCell is the tool an agent at work or waiting on a person last reported running, greyed.
func toolCell(row Row) Cell {
	if row.Process != ProcessAlive || row.Tool == "" {
		return Cell{}
	}

	if row.Status != StatusThinking && row.Status != StatusWaiting {
		return Cell{}
	}

	return Cell{Text: row.Tool, Tone: paint.ToneMuted}
}

// HiddenCell is the mark a row renders once a person has taken it out of the sidebar.
func HiddenCell(row Row) Cell {
	return markCell(row.Hidden, glyph.Hidden, paint.ToneMuted)
}

// MailCell is the mark a row renders for mail its agent has not read.
func MailCell(row Row) Cell {
	return markCell(row.UnreadMail, glyph.Mail, paint.ToneMuted)
}

// DocumentCell is the mark a row renders once its agent has written a document for a person to read.
func DocumentCell(row Row) Cell {
	return markCell(row.Documents, glyph.Document, paint.ToneMuted)
}

// AutoMergeCell is the mark a row renders while the forge is set to merge its open pull request
// on its own.
func AutoMergeCell(row Row) Cell {
	return markCell(carriesWork(row) && row.AutoMerge && !isFinished(row.Forge), glyph.AutoMerge, paint.ToneMerged)
}

// isFinished reports whether the pull request in the state was merged or closed.
func isFinished(forge Forge) bool {
	return forge == ForgeMerged || forge == ForgeClosed
}

// AlertCell is the mark a row renders for a script komrad fired that failed.
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

// chromeIf is the chrome given when it is to be rendered, and nothing when it is not.
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
func weightFor(kind RowKind) weight {
	switch kind {
	case KindWorkflow:
		return weightBold
	case KindAgent:
		return weightRegular
	case KindChild:
		return weightItalic
	case KindResult:
		return weightRegular
	case kindHit:
		return weightRegular
	case KindTask:
		return weightRegular
	case KindTriageMore, KindDivider:
		return weightRegular
	}

	return weightRegular
}

// indentFor is how far a row's name is set in from the gutter, an agent under its workflow and
// a child under its agent, and none where a tree is drawn in its place.
func indentFor(row Row) int {
	if row.Tree != nil || row.Kind == KindWorkflow || row.Kind == KindResult || row.Kind == KindDivider {
		return 0
	}

	return indentColumns
}

// branchFor is the tree drawn before a row's name: a rail down past each row it hangs under that
// has siblings still to come, then the corner it hangs off.
func branchFor(tree *Tree) string {
	if tree == nil {
		return ""
	}

	var b strings.Builder
	for _, last := range tree.Above {
		if last {
			b.WriteString("   ")
		} else {
			b.WriteString(glyph.Divider + "  ")
		}
	}

	if tree.Last {
		b.WriteString(glyph.Child + glyph.Rule + " ")
	} else {
		b.WriteString(glyph.TeeLeft + glyph.Rule + " ")
	}

	return b.String()
}

// fitRow gives a row's width up in order: the tool, the workflow, the tally, the comment counts, the name,
// and last the marks, which only a pane too narrow for the marks alone ever reaches.
func fitRow(line line, width int) line {
	if roomForName(line, width) < len([]rune(line.Name)) {
		line.Tool = Cell{}
	}

	if roomForName(line, width) < len([]rune(line.Name)) {
		line.Workflow = ""
	}

	if roomForName(line, width) <= 0 {
		line.Tally = 0
	}

	if roomForName(line, width) <= 0 {
		line.Comments = nil
	}

	if line.Editing {
		line.Name, line.Behind = clipNameAround(line.Name, line.Behind, roomForName(line, width))
	} else {
		line.Name, line.Matches = clipName(line.Name, line.Matches, roomForName(line, width))
	}

	return shedMarks(line, width)
}

// shedMarks gives the marks after a name up, in the order of what a person can do without,
// where even a nameless row will not fit the pane.
func shedMarks(line line, width int) line {
	// The schedule, the documents and the auto-merge are standing facts about the row, hiding is
	// something a person did themselves, the loader only says what the name already did, mail goes
	// before the output nobody has read, which asks the person to look, and the failed run nobody
	// else will mention goes last.
	for _, shed := range []*Cell{&line.Schedule, &line.Document, &line.AutoMerge, &line.Hidden, &line.Working, &line.Mail, &line.Alert} {
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

// emphasisFor is how brightly a name renders: the row under the cursor beating greying, and
// greying beating an agent waiting on a person or broken.
func emphasisFor(row Row, focused bool) emphasis {
	switch {
	case focused:
		return emphasisFocused
	case isDim(row):
		return EmphasisDim
	case row.Process != ProcessAlive:
		return emphasisNormal
	case row.Status == StatusWaiting || row.Status == StatusBroken:
		return emphasisAlert
	}

	return emphasisNormal
}

// clipNameAround cuts a name being typed to fit around its cursor, along with how many runes
// of what is left follow the cursor.
func clipNameAround(name string, behind int, width int) (string, int) {
	runes := []rune(name)
	at := min(max(len(runes)-behind, 0), len(runes))

	before, after := paint.ClipAround(string(runes[:at]), string(runes[at:]), width)

	return before + after, len([]rune(after))
}
