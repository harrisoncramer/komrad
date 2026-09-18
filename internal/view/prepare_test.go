package view

import (
	"testing"

	"weave/internal/ui/paint"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// prepareOne runs one row through Prepare and hands back the single line it made.
func prepareOne(t *testing.T, row Row) Line {
	t.Helper()

	list := Prepare(Snapshot{Rows: []Row{row}})
	require.Len(t, list.Lines, 1)

	return list.Lines[0]
}

// TestPrepareStatusBar pins the bar every state of an agent is drawn as.
func TestPrepareStatusBar(t *testing.T) {
	tests := []struct {
		name   string
		status Status
		want   Cell
	}{
		{
			name:   "a broken agent gets the danger bar",
			status: StatusBroken,
			want:   Cell{Glyph: GlyphBusy, Tone: paint.ToneDanger},
		},
		{
			name:   "an agent that wants you gets the warning bar",
			status: StatusWaiting,
			want:   Cell{Glyph: GlyphBusy, Tone: paint.ToneWarning},
		},
		{
			name:   "a thinking agent gets the accent bar",
			status: StatusThinking,
			want:   Cell{Glyph: GlyphBusy, Tone: paint.ToneAccent},
		},
		{
			name:   "an idle agent gets the muted pipe",
			status: StatusIdle,
			want:   Cell{Glyph: GlyphQuiet, Tone: paint.ToneMuted},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, prepareOne(t, Row{Status: tt.status}).Status)
		})
	}
}

// TestPrepareRepoColumn pins where an agent's own checkout stands.
func TestPrepareRepoColumn(t *testing.T) {
	tests := []struct {
		name string
		repo Repo
		want Cell
	}{
		{
			name: "a checkout that is gone gets the broken glyph in danger",
			repo: RepoGone,
			want: Cell{Glyph: GlyphBroken, Tone: paint.ToneDanger},
		},
		{
			name: "a rebase part-way through keeps the glyph and takes the warning tone",
			repo: RepoRebasing,
			want: Cell{Glyph: GlyphRepo, Tone: paint.ToneWarning},
		},
		{
			name: "a clean checkout is drawn greyed rather than left out",
			repo: RepoClean,
			want: Cell{Glyph: GlyphRepo, Tone: paint.ToneMuted},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, prepareOne(t, Row{Repo: tt.repo}).Repo)
		})
	}
}

// TestPrepareReviewColumn pins where the local review of an agent's work stands.
func TestPrepareReviewColumn(t *testing.T) {
	tests := []struct {
		name   string
		review Review
		want   Cell
	}{
		{
			name:   "changes requested gets its own glyph in warning",
			review: ReviewChangesRequested,
			want:   Cell{Glyph: GlyphChanges, Tone: paint.ToneWarning},
		},
		{
			name:   "approved gets the check in success",
			review: ReviewApproved,
			want:   Cell{Glyph: GlyphApproved, Tone: paint.ToneSuccess},
		},
		{
			name:   "an open review keeps the glyph and takes the accent tone",
			review: ReviewOpen,
			want:   Cell{Glyph: GlyphReview, Tone: paint.ToneAccent},
		},
		{
			name:   "no review at all is drawn greyed rather than left out",
			review: ReviewNone,
			want:   Cell{Glyph: GlyphReview, Tone: paint.ToneMuted},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, prepareOne(t, Row{Review: tt.review}).Review)
		})
	}
}

// TestPrepareForgeColumn pins where a pull request opened from the branch stands.
func TestPrepareForgeColumn(t *testing.T) {
	tests := []struct {
		name  string
		forge Forge
		want  Cell
	}{
		{
			name:  "failing checks get the broken glyph in danger",
			forge: ForgeFailing,
			want:  Cell{Glyph: GlyphBroken, Tone: paint.ToneDanger},
		},
		{
			name:  "a merged change gets its own glyph and its own tone",
			forge: ForgeMerged,
			want:  Cell{Glyph: GlyphMerged, Tone: paint.ToneMerged},
		},
		{
			name:  "approved gets the check in success",
			forge: ForgeApproved,
			want:  Cell{Glyph: GlyphApproved, Tone: paint.ToneSuccess},
		},
		{
			name:  "an open pull request keeps the glyph and takes the accent tone",
			forge: ForgeOpen,
			want:  Cell{Glyph: GlyphForge, Tone: paint.ToneAccent},
		},
		{
			name:  "no pull request at all is drawn greyed rather than left out",
			forge: ForgeNone,
			want:  Cell{Glyph: GlyphForge, Tone: paint.ToneMuted},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, prepareOne(t, Row{Forge: tt.forge}).Forge)
		})
	}
}

// TestPrepareWeightAndIndent pins the structure a row carries in its lettering rather than in a glyph.
func TestPrepareWeightAndIndent(t *testing.T) {
	tests := []struct {
		name       string
		kind       RowKind
		wantWeight Weight
		wantIndent int
		wantRule   bool
	}{
		{
			name:       "a child is italic, indented, and hangs off a corner",
			kind:       KindChild,
			wantWeight: WeightItalic,
			wantIndent: agentIndent,
			wantRule:   true,
		},
		{
			name:       "an ordinary agent is regular and indented under its workflow",
			kind:       KindAgent,
			wantWeight: WeightRegular,
			wantIndent: agentIndent,
			wantRule:   false,
		},
		{
			name:       "a workflow is bold and opens at the gutter",
			kind:       KindWorkflow,
			wantWeight: WeightBold,
			wantIndent: 0,
			wantRule:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := prepareOne(t, Row{Kind: tt.kind})

			assert.Equal(t, tt.wantWeight, line.Weight)
			assert.Equal(t, tt.wantIndent, line.Indent)
			assert.Equal(t, tt.wantRule, line.Rule)
		})
	}
}

// TestPrepareEmphasis pins how brightly a name is drawn.
func TestPrepareEmphasis(t *testing.T) {
	tests := []struct {
		name string
		row  Row
		want Emphasis
	}{
		{
			name: "a hidden row that is also unviewed stays dim, since dim is the stronger fact",
			row:  Row{Dim: true, Unviewed: true},
			want: EmphasisDim,
		},
		{
			name: "a hidden row is dim",
			row:  Row{Dim: true},
			want: EmphasisDim,
		},
		{
			name: "an unviewed row is brighter rather than bold",
			row:  Row{Unviewed: true},
			want: EmphasisBright,
		},
		{
			name: "an ordinary row is neither",
			row:  Row{},
			want: EmphasisNormal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, prepareOne(t, tt.row).Emphasis)
		})
	}
}

// TestPrepareDimRowGreysEveryCell pins the one rule that reaches across a whole row, so a
// hidden agent cannot keep a red bar or a green check.
func TestPrepareDimRowGreysEveryCell(t *testing.T) {
	line := prepareOne(t, Row{
		Dim:        true,
		Status:     StatusBroken,
		Repo:       RepoGone,
		Review:     ReviewApproved,
		Forge:      ForgeMerged,
		Held:       true,
		UnreadMail: true,
		FailedRun:  true,
		Scheduled:  true,
	})

	for _, cell := range []Cell{line.Status, line.Repo, line.Review, line.Forge, line.Held, line.Mail, line.Alert, line.Schedule} {
		assert.Equal(t, paint.ToneMuted, cell.Tone)
	}
}

// TestPrepareDimRowKeepsEveryGlyph pins that greying a row changes its colour and nothing else,
// so a hidden agent still says what is wrong with it.
func TestPrepareDimRowKeepsEveryGlyph(t *testing.T) {
	row := Row{
		Status:     StatusBroken,
		Repo:       RepoGone,
		Review:     ReviewApproved,
		Forge:      ForgeMerged,
		Held:       true,
		UnreadMail: true,
		FailedRun:  true,
		Scheduled:  true,
	}

	lit, dimmed := prepareOne(t, row), prepareOne(t, Row{
		Dim:        true,
		Status:     row.Status,
		Repo:       row.Repo,
		Review:     row.Review,
		Forge:      row.Forge,
		Held:       row.Held,
		UnreadMail: row.UnreadMail,
		FailedRun:  row.FailedRun,
		Scheduled:  row.Scheduled,
	})

	assert.Equal(t, lit.Status.Glyph, dimmed.Status.Glyph)
	assert.Equal(t, lit.Repo.Glyph, dimmed.Repo.Glyph)
	assert.Equal(t, lit.Review.Glyph, dimmed.Review.Glyph)
	assert.Equal(t, lit.Forge.Glyph, dimmed.Forge.Glyph)
	assert.Equal(t, lit.Held.Glyph, dimmed.Held.Glyph)
	assert.Equal(t, lit.Mail.Glyph, dimmed.Mail.Glyph)
	assert.Equal(t, lit.Alert.Glyph, dimmed.Alert.Glyph)
	assert.Equal(t, lit.Schedule.Glyph, dimmed.Schedule.Glyph)
}

// TestPrepareTrailingMarks pins the odds and ends drawn after a name.
func TestPrepareTrailingMarks(t *testing.T) {
	tests := []struct {
		name string
		row  Row
		want Line
	}{
		{
			name: "a row with nothing after its name draws no trailing cell at all",
			row:  Row{},
			want: Line{},
		},
		{
			name: "a failed nobody has read is the danger alert",
			row:  Row{FailedRun: true},
			want: Line{Alert: Cell{Glyph: GlyphAlert, Tone: paint.ToneDanger}},
		},
		{
			name: "unread mail is the accent envelope",
			row:  Row{UnreadMail: true},
			want: Line{Mail: Cell{Glyph: GlyphMail, Tone: paint.ToneAccent}},
		},
		{
			name: "a paused agent is the warning pause",
			row:  Row{Held: true},
			want: Line{Held: Cell{Glyph: GlyphPaused, Tone: paint.ToneWarning}},
		},
		{
			name: "a schedule targeting the row is the muted calendar",
			row:  Row{Scheduled: true},
			want: Line{Schedule: Cell{Glyph: GlyphSchedule, Tone: paint.ToneMuted}},
		},
		{
			name: "rows standing in for hidden ones carry the count",
			row:  Row{HiddenCount: 3},
			want: Line{Tally: 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := prepareOne(t, tt.row)

			assert.Equal(t, tt.want.Held, line.Held)
			assert.Equal(t, tt.want.Mail, line.Mail)
			assert.Equal(t, tt.want.Alert, line.Alert)
			assert.Equal(t, tt.want.Schedule, line.Schedule)
			assert.Equal(t, tt.want.Tally, line.Tally)
		})
	}
}

// TestPrepareFocus pins which row the cursor lands on.
func TestPrepareFocus(t *testing.T) {
	tests := []struct {
		name   string
		cursor int
		want   []bool
	}{
		{
			name:   "a cursor past the end focuses nothing",
			cursor: 7,
			want:   []bool{false, false, false},
		},
		{
			name:   "a cursor before the start focuses nothing",
			cursor: -1,
			want:   []bool{false, false, false},
		},
		{
			name:   "the cursor focuses the row it stands on and no other",
			cursor: 1,
			want:   []bool{false, true, false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := Prepare(Snapshot{
				Rows:   []Row{{Name: "one"}, {Name: "two"}, {Name: "three"}},
				Cursor: tt.cursor,
			})
			require.Len(t, list.Lines, len(tt.want))

			for i, want := range tt.want {
				assert.Equal(t, want, list.Lines[i].Focused)
			}
		})
	}
}

// TestPrepareClipsName pins the column a name is cut to.
func TestPrepareClipsName(t *testing.T) {
	tests := []struct {
		name      string
		given     string
		nameWidth int
		want      string
	}{
		{
			name:      "a name past the column ends in an ellipsis",
			given:     "eng-6994-unable-to-create",
			nameWidth: 12,
			want:      "eng-6994-un…",
		},
		{
			name:      "a name inside the column is left alone",
			given:     "iss-582",
			nameWidth: 12,
			want:      "iss-582",
		},
		{
			name:      "an unset column falls back to the default rather than cutting to nothing",
			given:     "research-identity-assets",
			nameWidth: 0,
			want:      "research-identity-assets",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := Prepare(Snapshot{Rows: []Row{{Name: tt.given}}, NameWidth: tt.nameWidth})
			require.Len(t, list.Lines, 1)

			assert.Equal(t, tt.want, list.Lines[0].Name)
		})
	}
}

// TestPrepareCarriesRowFacts pins the fields Prepare passes through untouched, which nothing
// else in the package decides.
func TestPrepareCarriesRowFacts(t *testing.T) {
	line := prepareOne(t, Row{Name: "strip-demo", Mark: "a", Selected: true})

	assert.Equal(t, "strip-demo", line.Name)
	assert.Equal(t, "a", line.Mark)
	assert.True(t, line.Selected)
}
