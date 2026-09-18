package view

import (
	"regexp"
	"strings"
	"testing"

	"weave/internal/ui/paint"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// canonicalPane is the sidebar the settled design was drawn in, wide enough for all three
// columns and the names beside them.
const canonicalPane = 39

// everyColumn is all three columns, written out here rather than taken from the package so a
// test measuring the gutter does not borrow the package's own idea of what fills it.
func everyColumn() Columns {
	return Columns{Repo: true, Review: true, Forge: true}
}

// ansiEscapes matches the styling a rendered row carries, so a test about layout can take it
// off and read the columns alone.
var ansiEscapes = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// renderOne draws a single row and hands back the line it made.
func renderOne(t *testing.T, row Row) string {
	t.Helper()

	list := Prepare(Snapshot{Rows: []Row{row}, Pane: canonicalPane})
	require.Len(t, list.Lines, 1)

	return Render(list, paint.Colored, 0)
}

// TestRenderStatusBar draws the four bars an agent's state is carried by.
func TestRenderStatusBar(t *testing.T) {
	tests := []struct {
		name   string
		golden string
		status Status
	}{
		{
			name:   "broken",
			golden: "status-broken",
			status: StatusBroken,
		},
		{
			name:   "wants you",
			golden: "status-waiting",
			status: StatusWaiting,
		},
		{
			name:   "thinking",
			golden: "status-thinking",
			status: StatusThinking,
		},
		{
			name:   "idle",
			golden: "status-idle",
			status: StatusIdle,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireGolden(t, tt.golden, renderOne(t, Row{Name: "iss-582", Status: tt.status}))
		})
	}
}

// TestRenderRowShapes draws one row of each kind the tree holds.
func TestRenderRowShapes(t *testing.T) {
	tests := []struct {
		name   string
		golden string
		row    Row
	}{
		{
			name:   "a hidden row is dim throughout, its bar included",
			golden: "row-hidden",
			row:    Row{Kind: KindAgent, Name: "gone-worktree", Dim: true, Repo: RepoGone, Status: StatusBroken, FailedRun: true},
		},
		{
			name:   "a child is italic and hangs off a corner",
			golden: "row-child",
			row:    Row{Kind: KindChild, Name: "eng-6994-reviewer"},
		},
		{
			name:   "a marked workflow is bold and opens where its letter leaves off",
			golden: "row-workflow",
			row:    Row{Kind: KindWorkflow, Name: "weave", Mark: "w", HiddenCount: 3, Scheduled: true},
		},
		{
			name:   "an unmarked workflow is bold and opens where an agent does",
			golden: "row-workflow-unmarked",
			row:    Row{Kind: KindWorkflow, Name: "weave-bughunt-job", HiddenCount: 5, Scheduled: true},
		},
		{
			name:   "an unviewed agent is brighter rather than bold",
			golden: "row-unviewed",
			row:    Row{Kind: KindAgent, Name: "frozen-review-bug", Unviewed: true, Status: StatusWaiting},
		},
		{
			name:   "a review verdict and a pull request stand in their own columns",
			golden: "row-reviewed",
			row:    Row{Kind: KindAgent, Name: "pr-metadata-fixes", Review: ReviewApproved, Forge: ForgeApproved, Status: StatusWaiting, Held: true},
		},
		{
			name:   "mail and a failed log follow the name",
			golden: "row-mail-and-alert",
			row:    Row{Kind: KindAgent, Name: "iss-641-reviewer", UnreadMail: true, FailedRun: true},
		},
		{
			name:   "a name past the column ends in an ellipsis",
			golden: "row-clipped",
			row:    Row{Kind: KindAgent, Name: "eng-6994-unable-to-create-a-grant"},
		},
		{
			name:   "a selected row is painted under a bar",
			golden: "row-selected",
			row:    Row{Kind: KindAgent, Name: "iss-582", Selected: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireGolden(t, tt.golden, renderOne(t, tt.row))
		})
	}
}

// TestNameColumnsTakesTheGutterOffThePane pins the arithmetic a caller sizes a name by, in the
// columns the font draws rather than the ones Unicode declares.
func TestNameColumnsTakesTheGutterOffThePane(t *testing.T) {
	tests := []struct {
		name string
		pane int
		want int
	}{
		{
			name: "a pane narrower than the gutter leaves no column at all",
			pane: 4,
			want: 0,
		},
		{
			name: "a pane exactly the gutter leaves no column at all",
			pane: GutterColumns(everyColumn()),
			want: 0,
		},
		{
			name: "a wider pane leaves what is left over",
			pane: GutterColumns(everyColumn()) + 24,
			want: 24,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, NameColumns(tt.pane, everyColumn()))
		})
	}
}

// TestRenderSelectedRowFillsThePane pins the bar a selected row is drawn under reaching the
// width it was given rather than stopping at the text.
func TestRenderSelectedRowFillsThePane(t *testing.T) {
	list := Prepare(Snapshot{
		Rows: []Row{{Kind: KindAgent, Name: "iss-582", Selected: true}},
		Pane: canonicalPane,
	})

	requireGolden(t, "row-selected-filled", Render(list, paint.Colored, 40))
}

// TestRenderSelectedRowUnderTheCursorKeepsBoth pins the row the two meet on: the bar spans it
// and the name under that bar still carries the cursor's own colour.
func TestRenderSelectedRowUnderTheCursorKeepsBoth(t *testing.T) {
	list := Prepare(Snapshot{
		Rows:    []Row{{Key: "iss-582", Kind: KindAgent, Name: "iss-582", Selected: true}},
		Focused: "iss-582",
		Pane:    canonicalPane,
	})

	drawn := Render(list, paint.Colored, 40)
	requireGolden(t, "row-selected-focused", drawn)

	// The golden alone would be satisfied by regenerating it, so the fact it is here for is
	// asserted against the escape the name itself opens in.
	assert.Contains(t, drawn, paint.BackgroundSequence(paint.SelectionBackground))

	unfocused := Render(Prepare(Snapshot{
		Rows: []Row{{Key: "iss-582", Kind: KindAgent, Name: "iss-582", Selected: true}},
		Pane: canonicalPane,
	}), paint.Colored, 40)

	assert.Contains(t, unfocused, paint.BackgroundSequence(paint.SelectionBackground))
	assert.NotEqual(t, styleOfName(unfocused, "iss-582"), styleOfName(drawn, "iss-582"),
		"the bar flattened the cursor off the name")
}

// TestRenderFocusedRow draws the row the keyboard is standing on.
func TestRenderFocusedRow(t *testing.T) {
	list := Prepare(Snapshot{
		Rows:    []Row{{Key: "strip-demo", Kind: KindAgent, Name: "strip-demo", Status: StatusWaiting, Held: true}},
		Focused: "strip-demo",
		Pane:    canonicalPane,
	})

	requireGolden(t, "row-focused", Render(list, paint.Colored, 0))
}

// emphasisRows is a list built to put all three emphases beside each other, which the settled
// design has no row for.
func emphasisRows() []Row {
	return []Row{
		{Kind: KindAgent, Name: "ordinary"},
		{Kind: KindAgent, Name: "unviewed", Unviewed: true},
		{Kind: KindAgent, Name: "hidden", Dim: true},
	}
}

// TestRenderEmphasisSideBySide draws the three emphases together in both palettes.
func TestRenderEmphasisSideBySide(t *testing.T) {
	tests := []struct {
		name    string
		golden  string
		palette paint.Palette
	}{
		{
			name:    "coloured",
			golden:  "list-emphasis",
			palette: paint.Colored,
		},
		{
			name:    "grey",
			golden:  "list-emphasis-grey",
			palette: paint.Grey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireGolden(t, tt.golden, Render(Prepare(Snapshot{Rows: emphasisRows()}), tt.palette, 0))
		})
	}
}

// TestRenderGreyingNeverDemotesAnUnviewedRow pins the one thing greying a region must not do:
// leave a row that has something to say worse off than the row beside it.
func TestRenderGreyingNeverDemotesAnUnviewedRow(t *testing.T) {
	lines := strings.Split(Render(Prepare(Snapshot{Rows: emphasisRows()}), paint.Grey, 0), "\n")
	require.Len(t, lines, 3)

	ordinary, unviewed, hidden := lines[0], lines[1], lines[2]

	assert.Equal(t, styleOfName(ordinary, "ordinary"), styleOfName(unviewed, "unviewed"))
	assert.NotEqual(t, styleOfName(ordinary, "ordinary"), styleOfName(hidden, "hidden"))
}

// styleOfName is the escape a drawn row opens its name with, empty when the name is drawn in
// whatever the terminal already uses.
func styleOfName(row, name string) string {
	at := strings.Index(row, name)
	if at < 0 {
		return ""
	}

	opened := ansiEscapes.FindAllString(row[:at], -1)
	if len(opened) == 0 {
		return ""
	}

	return opened[len(opened)-1]
}

// TestRenderGreyPaletteTakesEveryColourOff pins that a list drawn where the keys are not carries
// one colour throughout.
func TestRenderGreyPaletteTakesEveryColourOff(t *testing.T) {
	list := Prepare(Snapshot{Rows: canonicalRows(), Focused: "strip-demo", Pane: canonicalPane})

	requireGolden(t, "list-grey", Render(list, paint.Grey, 0))
}

// TestRenderCanonicalList draws the whole settled design.
func TestRenderCanonicalList(t *testing.T) {
	list := Prepare(Snapshot{Rows: canonicalRows(), Focused: "strip-demo", Pane: canonicalPane})

	requireGolden(t, "list-canonical", Render(list, paint.Colored, 0))
}

// livePanelRows is the panel as it actually stands, taken off Harry's own listing rather than
// drawn: mostly dormant agents with no bar at all, two running, one wanting a person and one
// that never started.
func livePanelRows() []Row {
	return []Row{
		{Key: "admin", Kind: KindWorkflow, Name: "admin", Mark: "a", Dormant: true},
		{Kind: KindAgent, Name: "eng-6994-unable-to-create-a-fundraising", Review: ReviewApproved, Dormant: true},
		{Kind: KindChild, Name: "eng-6994-reviewer", Dormant: true},
		{Kind: KindWorkflow, Name: "bcs-project", Dormant: true},
		{Kind: KindAgent, Name: "research-disbursements", Dormant: true},
		{Kind: KindAgent, Name: "research-identity-assets", Status: StatusThinking},
		{Kind: KindWorkflow, Name: "deploy", Dormant: true, HiddenCount: 2},
		{Kind: KindWorkflow, Name: "ops", Mark: "o", Dormant: true},
		{Kind: KindAgent, Name: "rds-backup-hardening", Dormant: true},
		{Kind: KindWorkflow, Name: "strip-demo", Dormant: true},
		{Key: "strip-demo", Kind: KindAgent, Name: "strip-demo", Review: ReviewChangesRequested, Status: StatusWaiting},
		{Kind: KindWorkflow, Name: "weave", Mark: "w", Dormant: true, HiddenCount: 4},
		{Kind: KindAgent, Name: "competitive-analysis", Dormant: true, Held: true},
		{Kind: KindChild, Name: "competitor-architecture", Dormant: true},
		{Kind: KindAgent, Name: "pr-metadata-fixes", Dormant: true, Held: true},
		{Kind: KindAgent, Name: "frozen-review-bug", Review: ReviewChangesRequested, Dormant: true},
		{Kind: KindAgent, Name: "view-foundation", Repo: RepoGone, Review: ReviewApproved, Dormant: true},
		{Kind: KindChild, Name: "view-foundation-2", Dormant: true},
		{Kind: KindAgent, Name: "view-gather", Repo: RepoRebasing, Review: ReviewOpen, Status: StatusThinking},
		{Kind: KindWorkflow, Name: "weave-bughunt-job", Dormant: true, Scheduled: true, HiddenCount: 5},
		{Kind: KindAgent, Name: "iss-641-reviewer", Status: StatusBroken},
	}
}

// TestRenderLivePanelShape draws the distribution a panel actually holds, which the settled
// design does not: it shows every value once, where a real listing is mostly empty bars.
func TestRenderLivePanelShape(t *testing.T) {
	list := Prepare(Snapshot{Rows: livePanelRows(), Focused: "strip-demo", Pane: canonicalPane})

	requireGolden(t, "list-live-panel", Render(list, paint.Colored, 0))
}

// TestRenderDrawsTheSpecifiedGlyphSequence pins the glyphs of the settled design and the order
// they come in, comparing runes rather than the columns they occupy.
func TestRenderDrawsTheSpecifiedGlyphSequence(t *testing.T) {
	want, err := readSpec()
	require.NoError(t, err)
	assert.Equal(t, want, strings.Split(drawCanonical(), "\n"))
}

// TestRenderIndentsEachKindUnderTheOneAbove pins the rule the design is built on: a workflow
// opens its name at the gutter, an agent one step in, and a child one step further.
func TestRenderIndentsEachKindUnderTheOneAbove(t *testing.T) {
	rows := canonicalRows()
	list := Prepare(Snapshot{Rows: rows, Focused: "strip-demo", Pane: canonicalPane})
	drawn := strings.Split(drawCanonical(), "\n")
	require.Len(t, drawn, len(list.Lines))

	for i, line := range list.Lines {
		assert.Equal(t, wantNameColumn(rows[i].Kind), nameColumnOf(drawn[i], line), "row %q", drawn[i])
	}
}

// TestRenderIndentsEveryKindBesideItsTwin pins the case the settled design cannot express on
// its own: each kind of row beside its marked and unmarked twin, indented alike.
func TestRenderIndentsEveryKindBesideItsTwin(t *testing.T) {
	rows := []Row{
		{Key: "marked-workflow", Kind: KindWorkflow, Name: "marked-workflow", Mark: "m"},
		{Key: "plain-workflow", Kind: KindWorkflow, Name: "plain-workflow"},
		{Key: "marked-agent", Kind: KindAgent, Name: "marked-agent", Mark: "a"},
		{Key: "plain-agent", Kind: KindAgent, Name: "plain-agent"},
		{Key: "marked-child", Kind: KindChild, Name: "marked-child", Mark: "c"},
		{Key: "plain-child", Kind: KindChild, Name: "plain-child"},
	}

	list := Prepare(Snapshot{Rows: rows, Focused: "plain-agent"})
	drawn := strings.Split(ansiEscapes.ReplaceAllString(Render(list, paint.Colored, 0), ""), "\n")
	require.Len(t, drawn, len(rows))

	for i, line := range list.Lines {
		assert.Equal(t, wantNameColumn(rows[i].Kind), nameColumnOf(drawn[i], line), "row %q", drawn[i])
	}
}

// TestRenderDormantRowKeepsTheNameColumn pins that the empty status cell holds its column, so a
// row with no bar opens its name where a row of its kind does.
func TestRenderDormantRowKeepsTheNameColumn(t *testing.T) {
	rows := []Row{
		{Key: "quiet", Kind: KindAgent, Name: "quiet"},
		{Key: "dormant", Kind: KindAgent, Name: "dormant", Dormant: true},
	}

	list := Prepare(Snapshot{Rows: rows})
	drawn := strings.Split(ansiEscapes.ReplaceAllString(Render(list, paint.Colored, 0), ""), "\n")
	require.Len(t, drawn, len(rows))

	for i, line := range list.Lines {
		assert.Equal(t, wantNameColumn(rows[i].Kind), nameColumnOf(drawn[i], line), "row %q", drawn[i])
	}
}

// TestRenderDrawsASearchMatchInTheMatchColour pins a filtered list still showing what it
// matched on, the rest of the name keeping its own lettering.
func TestRenderDrawsASearchMatchInTheMatchColour(t *testing.T) {
	row := Row{Key: "one", Kind: KindAgent, Name: "view-gather", Matches: []Span{{Start: 5, End: 11}}}

	drawn := renderOne(t, row)

	assert.Contains(t, drawn, paint.Colored.Match().Render("gather"))
	assert.Contains(t, ansiEscapes.ReplaceAllString(drawn, ""), "view-gather")
}

// TestNarrowSidebarsDropColumnsRatherThanNames pins what a sidebar gives up as it narrows: the
// columns go from the right, and a name is only cut once there is no column left to spend.
func TestNarrowSidebarsDropColumnsRatherThanNames(t *testing.T) {
	tests := []struct {
		name     string
		pane     int
		want     Columns
		wantName int
	}{
		{
			name:     "the width Harry's own config sets still draws the checkout, the column never given up",
			pane:     16,
			want:     Columns{Repo: true},
			wantName: 7,
		},
		{
			name:     "a little wider spends what it gains on the name rather than on a column",
			pane:     20,
			want:     Columns{Repo: true},
			wantName: 11,
		},
		{
			name:     "wider still buys back the review, the pull request being the last to come",
			pane:     24,
			want:     Columns{Repo: true, Review: true},
			wantName: 12,
		},
		{
			name:     "the default sidebar draws all three",
			pane:     30,
			want:     everyColumn(),
			wantName: 15,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := Prepare(Snapshot{Rows: livePanelRows(), Focused: "strip-demo", Pane: tt.pane})

			assert.Equal(t, tt.want, list.Columns)
			assert.Equal(t, tt.wantName, NameColumns(tt.pane, list.Columns))

			for i, drawn := range strings.Split(ansiEscapes.ReplaceAllString(Render(list, paint.Colored, 0), ""), "\n") {
				assert.LessOrEqual(t, drawnColumns(drawn), tt.pane, "row %q draws past the pane", drawn)
				assert.NotEmpty(t, list.Lines[i].Name, "row %q lost its name while a column was still drawn", drawn)
			}
		})
	}
}

// TestColumnsDoNotMoveWhenARowGainsAMark pins the column set being the pane's business alone: a
// list must not change shape because one agent was sent mail.
func TestColumnsDoNotMoveWhenARowGainsAMark(t *testing.T) {
	for pane := 16; pane <= 40; pane++ {
		quiet := livePanelRows()
		busy := livePanelRows()
		busy[2].UnreadMail = true
		busy[2].FailedRun = true
		busy[2].Held = true

		assert.Equal(t,
			Prepare(Snapshot{Rows: quiet, Pane: pane}).Columns,
			Prepare(Snapshot{Rows: busy, Pane: pane}).Columns,
			"the columns moved at pane %d because one row gained a mark", pane)
	}
}

// TestRenderKeepsEveryReachableRowInsideThePane pins the rule the whole layout rests on: no row
// draws past its pane, at any width, carrying anything the panel can actually put on it.
func TestRenderKeepsEveryReachableRowInsideThePane(t *testing.T) {
	for _, row := range reachableRows() {
		for pane := 16; pane <= 120; pane++ {
			list := Prepare(Snapshot{Rows: []Row{row}, Pane: pane})
			drawn := ansiEscapes.ReplaceAllString(Render(list, paint.Colored, 0), "")

			assert.LessOrEqual(t, drawnColumns(drawn), pane, "row %q draws past a pane of %d", drawn, pane)
		}
	}
}

// reachableRows is every combination of marks a row can actually carry, which is not every
// combination of the fields: a child carries neither a tally nor a schedule, since nothing is
// hidden behind it and a schedule targets a workflow rather than an agent.
func reachableRows() []Row {
	name := "an agent with a very long name indeed"

	var rows []Row
	for _, kind := range []RowKind{KindWorkflow, KindAgent, KindChild} {
		for marks := range 16 {
			row := Row{
				Kind:       kind,
				Name:       name,
				Held:       marks&1 != 0,
				UnreadMail: marks&2 != 0,
				FailedRun:  marks&4 != 0,
				Scheduled:  marks&8 != 0,
			}
			if kind == KindChild {
				row.Scheduled = false
			} else {
				row.HiddenCount = 12
			}

			rows = append(rows, row)
		}
	}

	return rows
}

// TestGutterColumnsAgreesWithWhatIsDrawn pins the arithmetic a caller sizes a name by against
// the row the renderer actually draws, so the two cannot drift apart.
func TestGutterColumnsAgreesWithWhatIsDrawn(t *testing.T) {
	assert.Equal(t, drawnNameColumn, GutterColumns(everyColumn()))
}

// drawnNameColumn is the column the settled design opens a workflow's name at, counted off the
// drawing by hand rather than from the package's own arithmetic.
const drawnNameColumn = 15

// drawnIndentStep is how far each level of the tree sets its name in, written out here rather
// than taken from the package so a change to the package's own constant fails these tests.
const drawnIndentStep = 2

// wantNameColumn is the column a row of the given kind opens its name at.
func wantNameColumn(kind RowKind) int {
	switch kind {
	case KindWorkflow:
		return drawnNameColumn
	case KindAgent:
		return drawnNameColumn + drawnIndentStep
	case KindChild:
		return drawnNameColumn + 2*drawnIndentStep
	}

	return drawnNameColumn
}

// octiconColumns is how wide an Octicon is drawn, written out here rather than taken from the
// package so a change to the package's own constant fails this test.
const octiconColumns = 2

// nameColumnOf is the column a drawn row opens its own name at, past the corner a child hangs
// off rather than at it.
func nameColumnOf(drawn string, line Line) int {
	at := strings.Index(drawn, line.Name)
	if at < 0 {
		return -1
	}

	return drawnColumns(drawn[:at])
}

// drawCanonical is the settled design as this package draws it, with its styling taken off.
func drawCanonical() string {
	list := Prepare(Snapshot{Rows: canonicalRows(), Focused: "strip-demo", Pane: canonicalPane})

	return ansiEscapes.ReplaceAllString(Render(list, paint.Colored, 0), "")
}

// drawnColumns is how wide a stretch of a row is on screen, counting an Octicon as what the font
// draws rather than as what Unicode declares.
func drawnColumns(text string) int {
	columns := 0
	for _, r := range text {
		if r >= 0xe000 && r <= 0xf8ff {
			columns += octiconColumns

			continue
		}

		columns++
	}

	return columns
}

// canonicalRows is the settled design as a snapshot, every row of it.
func canonicalRows() []Row {
	return []Row{
		{Key: "admin", Kind: KindWorkflow, Name: "admin", Mark: "a", HiddenCount: 2},
		{Kind: KindAgent, Name: "eng-6994-unable-to-create-a-grant", Review: ReviewApproved, Forge: ForgeOpen, Status: StatusThinking},
		{Kind: KindChild, Name: "eng-6994-reviewer"},
		{Kind: KindAgent, Name: "research-identity-assets", Status: StatusThinking},
		{Kind: KindWorkflow, Name: "ops", Mark: "o"},
		{Kind: KindAgent, Name: "rds-backup-hardening", Forge: ForgeOpen},
		{Key: "strip-demo", Kind: KindAgent, Name: "strip-demo", Review: ReviewChangesRequested, Status: StatusWaiting, Held: true},
		{Kind: KindWorkflow, Name: "weave", Mark: "w", HiddenCount: 3, Scheduled: true},
		{Kind: KindAgent, Name: "frozen-review-bug", Review: ReviewOpen, Status: StatusWaiting},
		{Kind: KindAgent, Name: "pr-metadata-fixes", Review: ReviewApproved, Forge: ForgeApproved, Status: StatusWaiting, Held: true, UnreadMail: true},
		{Kind: KindAgent, Name: "iss-582", Repo: RepoRebasing, Review: ReviewApproved},
		{Kind: KindAgent, Name: "gone-worktree", Repo: RepoGone, Status: StatusBroken, FailedRun: true},
		{Kind: KindAgent, Name: "iss-641-reviewer", Forge: ForgeFailing, UnreadMail: true},
		{Kind: KindAgent, Name: "shipped-yesterday", Forge: ForgeMerged},
		{Kind: KindWorkflow, Name: "weave-bughunt-job", HiddenCount: 5, Scheduled: true},
	}
}

// archivePane is the sidebar an archive row is drawn in here, wide enough that nothing below is
// the narrowing.
const archivePane = 39

// drawnArchiveNameColumn is the column a workflow opens its name at on a screen that draws none
// of the three columns, counted off the drawing by hand rather than from the package's own
// arithmetic: the caret, the mark and the bar, each a space apart, and a space before the name.
const drawnArchiveNameColumn = 6

// archiveRows is a list shaped like the archive: everything put away and so greyed, with a
// workflow still live above the agents archived out of it.
func archiveRows() []Row {
	return []Row{
		{Key: "w:live", Kind: KindWorkflow, Name: "weave", Dormant: true, Scheduled: true},
		{Key: "a:one", Kind: KindAgent, Name: "rip-out-old-sidebar", Dim: true, Dormant: true},
		{Key: "a:two", Kind: KindChild, Name: "rfc-0065-review", Dim: true, Dormant: true},
		{Key: "w:gone", Kind: KindWorkflow, Name: "spike", Dim: true, Dormant: true},
		{Key: "a:bad", Kind: KindAgent, Name: "failed-teardown", Dim: true, Status: StatusBroken},
	}
}

// TestRenderArchiveList draws the archive as the shared renderer makes it.
func TestRenderArchiveList(t *testing.T) {
	list := Prepare(Snapshot{
		Rows:    archiveRows(),
		Focused: "a:one",
		Pane:    archivePane,
		Dropped: Columns{Repo: true, Review: true, Forge: true},
	})

	requireGolden(t, "list-archive", Render(list, paint.Colored, archivePane))
}

// TestArchiveNameOpensPastTheGutterItHas pins the three sites that must agree once a screen gives
// its columns up: the drawing, the gutter arithmetic a caller sizes a name by, and the indent the
// tree is carried by.
func TestArchiveNameOpensPastTheGutterItHas(t *testing.T) {
	dropped := Columns{Repo: true, Review: true, Forge: true}
	list := Prepare(Snapshot{Rows: archiveRows(), Pane: archivePane, Dropped: dropped})
	require.Len(t, list.Lines, len(archiveRows()))

	assert.Equal(t, drawnArchiveNameColumn, GutterColumns(list.Columns))

	rows := archiveRows()
	for i, drawn := range Rows(list, paint.Colored, archivePane) {
		want := wantNameColumn(rows[i].Kind) - drawnNameColumn + drawnArchiveNameColumn

		assert.Equal(t, want, nameColumnOf(ansiEscapes.ReplaceAllString(drawn, ""), list.Lines[i]),
			"row %d (%q) opens its name where the gutter it has says it does", i, rows[i].Name)
	}
}
