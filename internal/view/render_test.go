package view

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"komrad/internal/ui/paint"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// canonicalPane is the sidebar the settled design was drawn in, wide enough for all three
// columns and the names beside them.
const canonicalPane = 36

// everyColumn is both glyph columns, written out here rather than taken from the package so a
// test measuring the gutter does not borrow the package's own idea of what fills it.
func everyColumn() Columns {
	return Columns{
		{Kind: ColumnRepo, Width: 1},
		{Kind: ColumnForge, Width: 1},
	}
}

// ansiEscapes matches the styling a rendered row carries, so a test about layout can take it
// off and read the columns alone.
var ansiEscapes = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// render draws every row of a list, one to a line.
func render(list List, palette paint.Palette, width int) string {
	return strings.Join(Rows(list, palette, width), "\n")
}

// renderOne draws a single row and hands back the line it made.
func renderOne(t *testing.T, row Row) string {
	t.Helper()

	list := Prepare(Snapshot{Rows: []Row{row}, Pane: canonicalPane})
	require.Len(t, list.Lines, 1)

	return render(list, paint.Panel, 0)
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
			name:   "a hidden row still running is bright and carries the mark",
			golden: "row-hidden",
			row: Row{
				Kind:      KindAgent,
				Name:      "gone-worktree",
				Hidden:    true,
				Repo:      RepoGone,
				Status:    StatusBroken,
				FailedRun: true,
			},
		},
		{
			name:   "a hidden row whose process has gone is dim throughout and keeps the mark",
			golden: "row-hidden-dormant",
			row: Row{
				Kind:      KindAgent,
				Name:      "gone-worktree",
				Hidden:    true,
				Process:   ProcessStopped,
				Repo:      RepoGone,
				FailedRun: true,
			},
		},
		{
			name:   "an archived row is dim throughout and carries no mark",
			golden: "row-archived",
			row: Row{
				Kind:      KindAgent,
				Name:      "gone-worktree",
				Archived:  true,
				Repo:      RepoGone,
				Status:    StatusBroken,
				FailedRun: true,
			},
		},
		{
			name:   "a child is italic and hangs off a corner",
			golden: "row-child",
			row:    Row{Kind: KindChild, Name: "eng-6994-reviewer"},
		},
		{
			name:   "a marked workflow is bold and opens where its letter leaves off",
			golden: "row-workflow",
			row: Row{
				Kind:        KindWorkflow,
				Name:        "komrad",
				Mark:        "w",
				HiddenCount: 3,
				Scheduled:   true,
			},
		},
		{
			name:   "an unmarked workflow is bold and opens where an agent does",
			golden: "row-workflow-unmarked",
			row: Row{
				Kind:        KindWorkflow,
				Name:        "komrad-bughunt-job",
				HiddenCount: 5,
				Scheduled:   true,
			},
		},
		{
			name:   "a review verdict and a pull request stand in their own columns",
			golden: "row-reviewed",
			row: Row{
				Kind:   KindAgent,
				Name:   "pr-metadata-fixes",
				Forge:  ForgeApproved,
				Status: StatusWaiting,
				Held:   true,
			},
		},
		{
			name:   "comment counts stand between the review and the pull request",
			golden: "row-commented",
			row: Row{
				Kind:   KindAgent,
				Name:   "hash-rewrite",
				Forge:  ForgeOpen,
				Status: StatusWaiting,
				ReviewerComments: &Comments{
					Open:     2,
					Resolved: 1,
				},
				PersonComments: &Comments{
					Open:   1,
					Unread: true,
				},
			},
		},
		{
			name:   "two-digit counts widen their columns rather than clipping",
			golden: "row-commented-wide",
			row: Row{
				Kind:   KindAgent,
				Name:   "hash-rewrite",
				Forge:  ForgeOpen,
				Status: StatusWaiting,
				ReviewerComments: &Comments{
					Open:     12,
					Resolved: 12,
				},
				PersonComments: &Comments{
					Open:     1,
					Resolved: 10,
				},
			},
		},
		{
			name:   "mail and a failed log follow the name",
			golden: "row-mail-and-alert",
			row: Row{
				Kind:       KindAgent,
				Name:       "iss-641-reviewer",
				UnreadMail: true,
				FailedRun:  true,
			},
		},
		{
			name:   "a name past the column ends in an ellipsis",
			golden: "row-clipped",
			row:    Row{Kind: KindAgent, Name: "eng-6994-unable-to-create-a-grant"},
		},
		{
			name:   "a selected row is painted under a bar",
			golden: "row-selected",
			row: Row{
				Kind:     KindAgent,
				Name:     "iss-582",
				Selected: true,
			},
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
		Rows: []Row{{
			Kind:     KindAgent,
			Name:     "iss-582",
			Selected: true,
		}},
		Pane: canonicalPane,
	})

	requireGolden(t, "row-selected-filled", render(list, paint.Panel, 40))
}

// TestRenderSelectedRowUnderTheCursorKeepsBoth pins the row the two meet on: the bar spans it
// and the name under that bar still carries the cursor's own colour.
func TestRenderSelectedRowUnderTheCursorKeepsBoth(t *testing.T) {
	list := Prepare(Snapshot{
		Rows: []Row{{
			Key:      "iss-582",
			Kind:     KindAgent,
			Name:     "iss-582",
			Selected: true,
		}},
		Focused: "iss-582",
		Pane:    canonicalPane,
	})

	drawn := render(list, paint.Panel, 40)
	requireGolden(t, "row-selected-focused", drawn)

	// The golden alone would be satisfied by regenerating it, so the fact it is here for is
	// asserted against the escape the name itself opens in.
	assert.Contains(t, drawn, paint.BackgroundSequence(paint.SelectionBackground))

	unfocused := render(Prepare(Snapshot{
		Rows: []Row{{
			Key:      "iss-582",
			Kind:     KindAgent,
			Name:     "iss-582",
			Selected: true,
		}},
		Pane: canonicalPane,
	}), paint.Panel, 40)

	assert.Contains(t, unfocused, paint.BackgroundSequence(paint.SelectionBackground))
	assert.NotEqual(t, styleOfName(unfocused, "iss-582"), styleOfName(drawn, "iss-582"),
		"the bar flattened the cursor off the name")
}

// TestRenderFocusedRow draws the row the keyboard is standing on.
func TestRenderFocusedRow(t *testing.T) {
	list := Prepare(Snapshot{
		Rows: []Row{{
			Key:    "strip-demo",
			Kind:   KindAgent,
			Name:   "strip-demo",
			Status: StatusWaiting,
			Held:   true,
		}},
		Focused: "strip-demo",
		Pane:    canonicalPane,
	})

	requireGolden(t, "row-focused", render(list, paint.Panel, 0))
}

// emphasisRows is a list built to put the emphases beside each other.
func emphasisRows() []Row {
	return []Row{
		{Kind: KindAgent, Name: "ordinary"},
		{
			Kind:    KindAgent,
			Name:    "dormant",
			Process: ProcessStopped,
		},
	}
}

// TestRenderEmphasis draws the emphases together.
func TestRenderEmphasis(t *testing.T) {
	requireGolden(t, "list-emphasis", render(Prepare(Snapshot{Rows: emphasisRows()}), paint.Panel, 0))
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

// TestRenderCanonicalList draws the whole settled design.
func TestRenderCanonicalList(t *testing.T) {
	list := Prepare(Snapshot{
		Rows:    canonicalRows(),
		Focused: "strip-demo",
		Pane:    canonicalPane,
	})

	requireGolden(t, "list-canonical", render(list, paint.Panel, 0))
}

// livePanelRows is the panel as it actually stands, taken off Harry's own listing rather than
// drawn: mostly dormant agents, two running, one wanting a person and one that never started.
func livePanelRows() []Row {
	return []Row{
		{
			Key:     "admin",
			Kind:    KindWorkflow,
			Name:    "admin",
			Mark:    "a",
			Process: ProcessNone,
		},
		{
			Kind:    KindAgent,
			Name:    "eng-6994-unable-to-create-a-fundraising",
			Process: ProcessStopped,
		},
		{
			Kind:    KindChild,
			Name:    "eng-6994-reviewer",
			Process: ProcessStopped,
		},
		{
			Kind:    KindWorkflow,
			Name:    "bcs-project",
			Process: ProcessNone,
		},
		{
			Kind:      KindAgent,
			Name:      "research-disbursements",
			Process:   ProcessStopped,
			Documents: true,
		},
		{
			Kind:   KindAgent,
			Name:   "research-identity-assets",
			Status: StatusThinking,
		},
		{
			Kind:        KindWorkflow,
			Name:        "deploy",
			Process:     ProcessNone,
			HiddenCount: 2,
		},
		{
			Kind:    KindWorkflow,
			Name:    "ops",
			Mark:    "o",
			Process: ProcessNone,
		},
		{
			Kind:    KindAgent,
			Name:    "rds-backup-hardening",
			Process: ProcessStopped,
		},
		{
			Kind:    KindWorkflow,
			Name:    "strip-demo",
			Process: ProcessNone,
		},
		{
			Key:    "strip-demo",
			Kind:   KindAgent,
			Name:   "strip-demo",
			Status: StatusWaiting,
		},
		{
			Kind:        KindWorkflow,
			Name:        "komrad",
			Mark:        "w",
			Process:     ProcessNone,
			HiddenCount: 4,
		},
		{
			Kind:    KindAgent,
			Name:    "competitive-analysis",
			Process: ProcessStopped,
			Held:    true,
		},
		{
			Kind:    KindChild,
			Name:    "competitor-architecture",
			Process: ProcessStopped,
		},
		{
			Kind:    KindAgent,
			Name:    "pr-metadata-fixes",
			Process: ProcessStopped,
			Held:    true,
		},
		{
			Kind:    KindAgent,
			Name:    "frozen-review-bug",
			Process: ProcessStopped,
		},
		{
			Kind:    KindAgent,
			Name:    "view-foundation",
			Repo:    RepoGone,
			Process: ProcessStopped,
		},
		{
			Kind:    KindChild,
			Name:    "view-foundation-2",
			Process: ProcessStopped,
		},
		{
			Kind:   KindAgent,
			Name:   "view-gather",
			Repo:   RepoRebasing,
			Status: StatusThinking,
		},
		{
			Kind:        KindWorkflow,
			Name:        "komrad-bughunt-job",
			Process:     ProcessNone,
			Scheduled:   true,
			HiddenCount: 5,
		},
		{
			Kind:   KindAgent,
			Name:   "iss-641-reviewer",
			Status: StatusBroken,
		},
	}
}

// TestRenderLivePanelShape draws the distribution a panel actually holds, which the settled
// design does not: it shows every value once, where a real listing is mostly empty bars.
func TestRenderLivePanelShape(t *testing.T) {
	list := Prepare(Snapshot{
		Rows:    livePanelRows(),
		Focused: "strip-demo",
		Pane:    canonicalPane,
	})

	requireGolden(t, "list-live-panel", render(list, paint.Panel, 0))
}

// TestRenderCommentCounts draws the comment columns in a narrow sidebar, blank for a side with no
// comments and left out of the gutter while no row has any.
func TestRenderCommentCounts(t *testing.T) {
	plain := Row{
		Kind:   KindAgent,
		Name:   "render-issues",
		Forge:  ForgeOpen,
		Status: StatusIdle,
	}
	reviewed := Row{
		Kind:   KindAgent,
		Name:   "hash-rewrite",
		Status: StatusWaiting,
		ReviewerComments: &Comments{
			Open:     2,
			Resolved: 1,
		},
	}
	commented := Row{
		Kind:           KindAgent,
		Name:           "gutter-fix",
		Status:         StatusIdle,
		PersonComments: &Comments{Resolved: 3},
	}
	both := Row{
		Kind:   KindAgent,
		Name:   "pr-state-notices",
		Forge:  ForgeDraft,
		Status: StatusThinking,
		ReviewerComments: &Comments{
			Open:     12,
			Resolved: 4,
		},
		PersonComments: &Comments{
			Open:   1,
			Unread: true,
		},
	}

	tests := []struct {
		name   string
		golden string
		rows   []Row
	}{
		{
			name:   "no comments on any row take no columns",
			golden: "list-comments-none",
			rows:   []Row{plain, plain},
		},
		{
			name:   "one side draws its column and a row without comments leaves it blank",
			golden: "list-comments-one-side",
			rows:   []Row{plain, reviewed},
		},
		{
			name:   "both sides keep their columns aligned across rows with either or neither",
			golden: "list-comments-both",
			rows:   []Row{plain, reviewed, commented, both},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := Prepare(Snapshot{
				Rows: tt.rows,
				Pane: 30,
			})

			requireGolden(t, tt.golden, render(list, paint.Panel, 0))
		})
	}
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
	list := Prepare(Snapshot{
		Rows:    rows,
		Focused: "strip-demo",
		Pane:    canonicalPane,
	})
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
		{
			Key:  "marked-workflow",
			Kind: KindWorkflow,
			Name: "marked-workflow",
			Mark: "m",
		},
		{
			Key:  "plain-workflow",
			Kind: KindWorkflow,
			Name: "plain-workflow",
		},
		{
			Key:  "marked-agent",
			Kind: KindAgent,
			Name: "marked-agent",
			Mark: "a",
		},
		{
			Key:  "plain-agent",
			Kind: KindAgent,
			Name: "plain-agent",
		},
		{
			Key:  "marked-child",
			Kind: KindChild,
			Name: "marked-child",
			Mark: "c",
		},
		{
			Key:  "plain-child",
			Kind: KindChild,
			Name: "plain-child",
		},
	}

	list := Prepare(Snapshot{Rows: rows, Focused: "plain-agent"})
	drawn := strings.Split(ansiEscapes.ReplaceAllString(render(list, paint.Panel, 0), ""), "\n")
	require.Len(t, drawn, len(rows))

	for i, line := range list.Lines {
		assert.Equal(t, wantNameColumn(rows[i].Kind), nameColumnOf(drawn[i], line), "row %q", drawn[i])
	}
}

// TestRenderBlankBarKeepsTheNameColumn pins that the empty status cell holds its column, so a
// row with no bar opens its name where a row of its kind does.
func TestRenderBlankBarKeepsTheNameColumn(t *testing.T) {
	rows := []Row{
		{
			Key:  "quiet",
			Kind: KindAgent,
			Name: "quiet",
		},
		{
			Key:     "barless",
			Kind:    KindAgent,
			Name:    "barless",
			Process: ProcessNone,
		},
	}

	list := Prepare(Snapshot{Rows: rows})
	drawn := strings.Split(ansiEscapes.ReplaceAllString(render(list, paint.Panel, 0), ""), "\n")
	require.Len(t, drawn, len(rows))

	for i, line := range list.Lines {
		assert.Equal(t, wantNameColumn(rows[i].Kind), nameColumnOf(drawn[i], line), "row %q", drawn[i])
	}
}

// TestRenderDrawsASearchMatchInTheMatchColour pins a filtered list still showing what it
// matched on, the rest of the name keeping its own lettering.
func TestRenderDrawsASearchMatchInTheMatchColour(t *testing.T) {
	row := Row{
		Key:     "one",
		Kind:    KindAgent,
		Name:    "view-gather",
		Matches: []Span{{Start: 5, End: 11}},
	}

	drawn := renderOne(t, row)

	assert.Contains(t, drawn, paint.Panel.Match().Render("gather"))
	assert.Contains(t, ansiEscapes.ReplaceAllString(drawn, ""), "view-gather")
}

// TestNarrowSidebarsKeepTheirColumns pins that a narrow sidebar cuts names rather than columns:
// the gutter a list declares is drawn at every pane.
func TestNarrowSidebarsKeepTheirColumns(t *testing.T) {
	tests := []struct {
		name     string
		pane     int
		wantName int
	}{
		{
			name:     "a sidebar narrower than a name keeps the columns and cuts the name",
			pane:     16,
			wantName: 6,
		},
		{
			name:     "the default sidebar draws both with room to spare",
			pane:     30,
			wantName: 20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := Prepare(Snapshot{
				Rows:    livePanelRows(),
				Focused: "strip-demo",
				Pane:    tt.pane,
			})

			assert.Equal(t, everyColumn(), list.Columns)
			assert.Equal(t, tt.wantName, NameColumns(tt.pane, list.Columns))

			for _, drawn := range strings.Split(ansiEscapes.ReplaceAllString(render(list, paint.Panel, 0), ""), "\n") {
				assert.LessOrEqual(t, drawnColumns(drawn), tt.pane, "row %q draws past the pane", drawn)
			}
		})
	}
}

// TestRenderKeepsEveryReachableRowInsideThePane pins the rule the whole layout rests on: no row
// draws past its pane, at any width, carrying anything the panel can actually put on it.
func TestRenderKeepsEveryReachableRowInsideThePane(t *testing.T) {
	for _, row := range reachableRows() {
		for pane := 16; pane <= 120; pane++ {
			list := Prepare(Snapshot{Rows: []Row{row}, Pane: pane})
			drawn := ansiEscapes.ReplaceAllString(render(list, paint.Panel, 0), "")

			assert.LessOrEqual(t, drawnColumns(drawn), pane, "row %q draws past a pane of %d", drawn, pane)
		}
	}
}

// reachableRows is every combination of marks a row can actually carry.
func reachableRows() []Row {
	name := "an agent with a very long name indeed"

	var rows []Row
	for _, kind := range []RowKind{KindWorkflow, KindAgent, KindChild} {
		for marks := range 64 {
			row := Row{
				Kind:       kind,
				Name:       name,
				Held:       marks&1 != 0,
				UnreadMail: marks&2 != 0,
				FailedRun:  marks&4 != 0,
				Scheduled:  marks&8 != 0,
				Hidden:     marks&16 != 0,
				Documents:  marks&32 != 0,
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
const drawnNameColumn = 10

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
	case KindResult:
		return drawnResultNameColumn
	case KindHit:
		return drawnResultNameColumn + drawnIndentStep
	}

	return drawnNameColumn
}

// TestEveryGlyphDrawsOneColumn pins the measurement the whole gutter rests on: an Octicon takes
// one column, the same as the box-drawing rules beside it.
func TestEveryGlyphDrawsOneColumn(t *testing.T) {
	for glyph := GlyphNone; glyph < glyphCount; glyph++ {
		spelled := Spell(glyph)
		if glyph == GlyphNone {
			assert.Empty(t, spelled)

			continue
		}

		require.NotEmpty(t, spelled, "glyph %d is spelled out nowhere", glyph)
		assert.Equal(t, plainColumns, lipgloss.Width(spelled), "glyph %q", spelled)
	}
}

// octiconColumns is how wide an Octicon is drawn, written out here rather than taken from the
// package so a change to the package's own constant fails this test.
const octiconColumns = 1

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
	list := Prepare(Snapshot{
		Rows:    canonicalRows(),
		Focused: "strip-demo",
		Pane:    canonicalPane,
	})

	return ansiEscapes.ReplaceAllString(render(list, paint.Panel, 0), "")
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
		{
			Key:         "admin",
			Kind:        KindWorkflow,
			Name:        "admin",
			Mark:        "a",
			HiddenCount: 2,
		},
		{
			Kind:   KindAgent,
			Name:   "eng-6994-unable-to-create-a-grant",
			Forge:  ForgeOpen,
			Status: StatusThinking,
		},
		{Kind: KindChild, Name: "eng-6994-reviewer"},
		{
			Kind:   KindAgent,
			Name:   "research-identity-assets",
			Status: StatusThinking,
		},
		{
			Kind: KindWorkflow,
			Name: "ops",
			Mark: "o",
		},
		{
			Kind:  KindAgent,
			Name:  "rds-backup-hardening",
			Forge: ForgeOpen,
		},
		{
			Key:    "strip-demo",
			Kind:   KindAgent,
			Name:   "strip-demo",
			Status: StatusWaiting,
			Held:   true,
		},
		{
			Kind:        KindWorkflow,
			Name:        "komrad",
			Mark:        "w",
			HiddenCount: 3,
			Scheduled:   true,
		},
		{
			Kind:   KindAgent,
			Name:   "frozen-review-bug",
			Status: StatusWaiting,
		},
		{
			Kind:       KindAgent,
			Name:       "pr-metadata-fixes",
			Forge:      ForgeApproved,
			Status:     StatusWaiting,
			Held:       true,
			UnreadMail: true,
		},
		{
			Kind:   KindAgent,
			Name:   "iss-582",
			Repo:   RepoRebasing,
			Hidden: true,
		},
		{
			Kind:      KindAgent,
			Name:      "gone-worktree",
			Repo:      RepoGone,
			Status:    StatusBroken,
			FailedRun: true,
		},
		{
			Kind:       KindAgent,
			Name:       "iss-641-reviewer",
			Forge:      ForgeFailing,
			UnreadMail: true,
		},
		{
			Kind:    KindAgent,
			Name:    "shipped-yesterday",
			Forge:   ForgeMerged,
			Process: ProcessStopped,
		},
		{
			Kind:        KindWorkflow,
			Name:        "komrad-bughunt-job",
			HiddenCount: 5,
			Scheduled:   true,
			Process:     ProcessNone,
		},
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
		{
			Key:       "w:live",
			Kind:      KindWorkflow,
			Name:      "komrad",
			Process:   ProcessNone,
			Scheduled: true,
		},
		{
			Key:      "a:one",
			Kind:     KindAgent,
			Name:     "rip-out-old-sidebar",
			Archived: true,
			Process:  ProcessStopped,
		},
		{
			Key:      "a:two",
			Kind:     KindChild,
			Name:     "rfc-0065-review",
			Archived: true,
			Process:  ProcessStopped,
		},
		{
			Key:      "w:gone",
			Kind:     KindWorkflow,
			Name:     "spike",
			Archived: true,
			Process:  ProcessNone,
		},
		{
			Key:      "a:bad",
			Kind:     KindAgent,
			Name:     "failed-teardown",
			Archived: true,
			Status:   StatusBroken,
		},
	}
}

// TestRenderArchiveList draws the archive as the shared renderer makes it.
func TestRenderArchiveList(t *testing.T) {
	list := Prepare(Snapshot{
		Rows:    archiveRows(),
		Focused: "a:one",
		Pane:    archivePane,
		Columns: NoColumns(),
	})

	requireGolden(t, "list-archive", render(list, paint.Panel, archivePane))
}

// TestArchiveNameOpensPastTheGutterItHas pins the three sites that must agree once a screen gives
// its columns up: the drawing, the gutter arithmetic a caller sizes a name by, and the indent the
// tree is carried by.
func TestArchiveNameOpensPastTheGutterItHas(t *testing.T) {
	list := Prepare(Snapshot{
		Rows:    archiveRows(),
		Pane:    archivePane,
		Columns: NoColumns(),
	})
	require.Len(t, list.Lines, len(archiveRows()))

	assert.Equal(t, drawnArchiveNameColumn, GutterColumns(list.Columns))

	rows := archiveRows()
	for i, drawn := range Rows(list, paint.Panel, archivePane) {
		want := wantNameColumn(rows[i].Kind) - drawnNameColumn + drawnArchiveNameColumn

		assert.Equal(t, want, nameColumnOf(ansiEscapes.ReplaceAllString(drawn, ""), list.Lines[i]),
			"row %d (%q) opens its name where the gutter it has says it does", i, rows[i].Name)
	}
}

// update rewrites every golden file from what the renderer draws.
var update = flag.Bool("update", false, "rewrite the golden files")

// TestMain pins the colour profile.
func TestMain(m *testing.M) {
	flag.Parse()

	os.Exit(m.Run())
}

// requireGolden compares a drawn block against the file holding it, writing that file instead
// when the suite was run to update them.
func requireGolden(t *testing.T, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", name+".golden")
	if *update {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o600))

		return
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err, "no golden file yet, so run the suite with -update")
	assert.Equal(t, string(want), got)
}

// specPath is the settled design as plain text, kept beside the package since the RFC it was
// drawn in is not tracked.
const specPath = "testdata/canonical-sidebar.golden"

// readSpec is the lines of the settled design, as columns with no styling on them.
func readSpec() ([]string, error) {
	raw, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read the settled design at %q: %w", specPath, err)
	}

	return strings.Split(strings.TrimRight(string(raw), "\n"), "\n"), nil
}

// figureColumns is a list shaped like the worktrees screen: a size then an age, both figures.
func figureColumns() Columns {
	return Columns{
		{Kind: ColumnText, Width: 4},
		{Kind: ColumnText, Width: 4},
	}
}

func muted(text string) Cell {
	return Cell{Text: text, Tone: paint.ToneMuted}
}

// TestRenderTextColumnsSitBesideTheBar draws a list declaring two figure columns: each figure
// set against the right edge of its column, a header carrying only the first, and a child
// carrying none.
func TestRenderTextColumnsSitBesideTheBar(t *testing.T) {
	rows := []Row{
		{
			Kind:  KindWorkflow,
			Name:  "~/komrad",
			Cells: []Cell{muted("24G")},
		},
		{
			Kind:  KindAgent,
			Name:  "worktree-view",
			Cells: []Cell{muted("49M"), muted("4d")},
		},
		{
			Kind:  KindAgent,
			Name:  "issue-607-panel-key-binding",
			Cells: []Cell{muted("9.2M"), muted("12h")},
		},
		{Kind: KindChild, Name: "review-607"},
		{
			Kind:    KindAgent,
			Name:    "gone",
			Process: ProcessStopped,
			Cells:   []Cell{muted("—"), muted("104w")},
		},
	}
	list := Prepare(Snapshot{
		Rows:    rows,
		Pane:    30,
		Columns: figureColumns(),
	})

	drawn := render(list, paint.Panel, 30)
	requireGolden(t, "list-figures", drawn)

	lines := strings.Split(ansiEscapes.ReplaceAllString(drawn, ""), "\n")
	require.Len(t, lines, len(rows))

	for i, line := range lines {
		assert.LessOrEqualf(t, lipgloss.Width(line), 30, "row %d draws past the pane", i)
		want := GutterColumns(figureColumns()) + wantNameColumn(rows[i].Kind) - drawnNameColumn
		assert.Equalf(t, want, nameColumnOf(line, list.Lines[i]), "row %d opens its name past the gutter", i)
	}

	assert.Equal(t, "     24G      │ ~/komrad", lines[0])
	assert.Equal(t, "    9.2M  12h │   issue-607-p…", lines[2])
	assert.Equal(t, "              │   └ review-607", lines[3])
}

// TestRenderTextCellIsClippedToItsColumn pins a figure wider than its column being cut rather
// than pushing the name along, and a column with no width drawing nothing at all.
func TestRenderTextCellIsClippedToItsColumn(t *testing.T) {
	tests := []struct {
		name  string
		width int
		want  string
	}{
		{
			name:  "a column of no width draws nothing",
			width: 0,
			want:  "     │ komrad",
		},
		{
			name:  "a figure wider than its column is cut",
			width: 4,
			want:  "    123… │ komrad",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := Prepare(Snapshot{
				Rows: []Row{{
					Kind:  KindWorkflow,
					Name:  "komrad",
					Cells: []Cell{muted("12345")},
				}},
				Columns: Columns{{Kind: ColumnText, Width: tt.width}},
			})

			drawn := ansiEscapes.ReplaceAllString(render(list, paint.Panel, 0), "")

			assert.Equal(t, tt.want, drawn)
		})
	}
}

// TestRenderResultList draws a result list whole.
func TestRenderResultList(t *testing.T) {
	list := Prepare(Snapshot{
		Rows:    resultRows(),
		Columns: NoColumns(),
		Focused: "live",
		Pane:    canonicalPane,
	})

	requireGolden(t, "list-results", render(list, paint.Panel, canonicalPane))
}
