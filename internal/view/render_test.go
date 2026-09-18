package view

import (
	"regexp"
	"strings"
	"testing"

	"weave/internal/ui/paint"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// canonicalNameWidth is the column the settled design cuts a name to.
const canonicalNameWidth = 24

// ansiEscapes matches the styling a rendered row carries, so a test about layout can take it
// off and read the columns alone.
var ansiEscapes = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// renderOne draws a single row and hands back the line it made.
func renderOne(t *testing.T, row Row) string {
	t.Helper()

	list := Prepare(Snapshot{Rows: []Row{row}, NameWidth: canonicalNameWidth})
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
			pane: GutterColumns(),
			want: 0,
		},
		{
			name: "a wider pane leaves what is left over",
			pane: GutterColumns() + 24,
			want: 24,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, NameColumns(tt.pane))
		})
	}
}

// TestRenderSelectedRowFillsThePane pins the bar a selected row is drawn under reaching the
// width it was given rather than stopping at the text.
func TestRenderSelectedRowFillsThePane(t *testing.T) {
	list := Prepare(Snapshot{
		Rows:      []Row{{Kind: KindAgent, Name: "iss-582", Selected: true}},
		NameWidth: canonicalNameWidth,
	})

	requireGolden(t, "row-selected-filled", Render(list, paint.Colored, 40))
}

// TestRenderFocusedRow draws the row the keyboard is standing on.
func TestRenderFocusedRow(t *testing.T) {
	list := Prepare(Snapshot{
		Rows:      []Row{{Key: "strip-demo", Kind: KindAgent, Name: "strip-demo", Status: StatusWaiting, Held: true}},
		Focused:   "strip-demo",
		NameWidth: canonicalNameWidth,
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
	list := Prepare(Snapshot{Rows: canonicalRows(), Focused: "strip-demo", NameWidth: canonicalNameWidth})

	requireGolden(t, "list-grey", Header(paint.Grey)+"\n"+Render(list, paint.Grey, 0))
}

// TestRenderCanonicalList draws the whole settled design.
func TestRenderCanonicalList(t *testing.T) {
	list := Prepare(Snapshot{Rows: canonicalRows(), Focused: "strip-demo", NameWidth: canonicalNameWidth})

	requireGolden(t, "list-canonical", Header(paint.Colored)+"\n"+Render(list, paint.Colored, 0))
}

// TestRenderDrawsTheSpecifiedGlyphSequence pins the glyphs of the settled design and the order
// they come in, comparing runes rather than the columns they occupy.
func TestRenderDrawsTheSpecifiedGlyphSequence(t *testing.T) {
	want, err := readSpec()
	require.NoError(t, err)
	assert.Equal(t, want, strings.Split(drawCanonical(), "\n"))
}

// TestRenderKeepsEveryNameOnOneColumn pins the rule the design is built on: every row, of every
// kind, marked or not, opens its name at the same column.
func TestRenderKeepsEveryNameOnOneColumn(t *testing.T) {
	list := Prepare(Snapshot{Rows: canonicalRows(), Focused: "strip-demo", NameWidth: canonicalNameWidth})
	drawn := strings.Split(drawCanonical(), "\n")
	require.Len(t, drawn, len(list.Lines)+1)

	for i, line := range list.Lines {
		row := drawn[i+1]
		assert.Equal(t, drawnNameColumn, nameColumnOf(row, line), "row %q", row)
	}
}

// TestGutterColumnsAgreesWithWhatIsDrawn pins the arithmetic a caller sizes a name by against
// the row the renderer actually draws, so the two cannot drift apart.
func TestGutterColumnsAgreesWithWhatIsDrawn(t *testing.T) {
	assert.Equal(t, drawnNameColumn, GutterColumns())
}

// drawnNameColumn is the column the settled design opens every name at, counted off the drawing
// by hand rather than from the package's own arithmetic.
const drawnNameColumn = 15

// octiconColumns is how wide an Octicon is drawn, written out here rather than taken from the
// package so a change to the package's own constant fails this test.
const octiconColumns = 2

// nameColumnOf is the column a drawn row opens its name cell at, which for a child is the corner
// it hangs off rather than the name itself.
func nameColumnOf(drawn string, line Line) int {
	opens := line.Name
	if line.Rule {
		opens = spell(GlyphChild)
	}

	at := strings.Index(drawn, opens)
	if at < 0 {
		return -1
	}

	return drawnColumns(drawn[:at])
}

// drawCanonical is the settled design as this package draws it, with its styling taken off.
func drawCanonical() string {
	list := Prepare(Snapshot{Rows: canonicalRows(), Focused: "strip-demo", NameWidth: canonicalNameWidth})

	return ansiEscapes.ReplaceAllString(Header(paint.Colored)+"\n"+Render(list, paint.Colored, 0), "")
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
