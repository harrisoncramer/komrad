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

	list := Prepare(Snapshot{Rows: []Row{row}, Cursor: -1, NameWidth: canonicalNameWidth})
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
			name:   "a workflow is bold and opens at the gutter",
			golden: "row-workflow",
			row:    Row{Kind: KindWorkflow, Name: "weave", Mark: "w", HiddenCount: 3, Scheduled: true},
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

// TestRenderSelectedRowFillsThePane pins the bar a selected row is drawn under reaching the
// width it was given rather than stopping at the text.
func TestRenderSelectedRowFillsThePane(t *testing.T) {
	list := Prepare(Snapshot{
		Rows:      []Row{{Kind: KindAgent, Name: "iss-582", Selected: true}},
		Cursor:    -1,
		NameWidth: canonicalNameWidth,
	})

	requireGolden(t, "row-selected-filled", Render(list, paint.Colored, 40))
}

// TestRenderFocusedRow draws the row the keyboard is standing on.
func TestRenderFocusedRow(t *testing.T) {
	list := Prepare(Snapshot{
		Rows:      []Row{{Kind: KindAgent, Name: "strip-demo", Status: StatusWaiting, Held: true}},
		NameWidth: canonicalNameWidth,
	})

	requireGolden(t, "row-focused", Render(list, paint.Colored, 0))
}

// TestRenderGreyPaletteTakesEveryColourOff pins that a list drawn where the keys are not carries
// one colour throughout.
func TestRenderGreyPaletteTakesEveryColourOff(t *testing.T) {
	list := Prepare(Snapshot{Rows: canonicalRows(), Cursor: 6, NameWidth: canonicalNameWidth})

	requireGolden(t, "list-grey", Header(paint.Grey)+"\n"+Render(list, paint.Grey, 0))
}

// TestRenderCanonicalList draws the whole settled design.
func TestRenderCanonicalList(t *testing.T) {
	list := Prepare(Snapshot{Rows: canonicalRows(), Cursor: 6, NameWidth: canonicalNameWidth})

	requireGolden(t, "list-canonical", Header(paint.Colored)+"\n"+Render(list, paint.Colored, 0))
}

// TestRenderMatchesTheSpecifiedLayout pins every column of the settled design against the design
// itself, read with its styling taken off so only the columns are compared.
func TestRenderMatchesTheSpecifiedLayout(t *testing.T) {
	list := Prepare(Snapshot{Rows: canonicalRows(), Cursor: 6, NameWidth: canonicalNameWidth})

	drawn := ansiEscapes.ReplaceAllString(Header(paint.Colored)+"\n"+Render(list, paint.Colored, 0), "")

	want, err := readSpec()
	require.NoError(t, err)
	assert.Equal(t, want, strings.Split(drawn, "\n"))
}

// canonicalRows is the settled design as a snapshot, every row of it.
func canonicalRows() []Row {
	return []Row{
		{Kind: KindWorkflow, Name: "admin", Mark: "a", HiddenCount: 2},
		{Kind: KindAgent, Name: "eng-6994-unable-to-create-a-grant", Review: ReviewApproved, Forge: ForgeOpen, Status: StatusThinking},
		{Kind: KindChild, Name: "eng-6994-reviewer"},
		{Kind: KindAgent, Name: "research-identity-assets", Status: StatusThinking},
		{Kind: KindWorkflow, Name: "ops", Mark: "o"},
		{Kind: KindAgent, Name: "rds-backup-hardening", Forge: ForgeOpen},
		{Kind: KindAgent, Name: "strip-demo", Review: ReviewChangesRequested, Status: StatusWaiting, Held: true},
		{Kind: KindWorkflow, Name: "weave", Mark: "w", HiddenCount: 3, Scheduled: true},
		{Kind: KindAgent, Name: "frozen-review-bug", Review: ReviewOpen, Status: StatusWaiting},
		{Kind: KindAgent, Name: "pr-metadata-fixes", Review: ReviewApproved, Forge: ForgeApproved, Status: StatusWaiting, Held: true, UnreadMail: true},
		{Kind: KindAgent, Name: "iss-582", Repo: RepoRebasing, Review: ReviewApproved},
		{Kind: KindAgent, Name: "gone-worktree", Repo: RepoGone, Status: StatusBroken, FailedRun: true},
		{Kind: KindAgent, Name: "iss-641-reviewer", Forge: ForgeFailing, UnreadMail: true},
		{Kind: KindAgent, Name: "shipped-yesterday", Forge: ForgeMerged},
	}
}
