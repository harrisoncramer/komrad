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

// TestPrepareDormantRowDrawsNoBar pins the one cell that says something by drawing nothing: an
// agent with a process behind it is quiet, and one with none has no channel at all.
func TestPrepareDormantRowDrawsNoBar(t *testing.T) {
	quiet := prepareOne(t, Row{Status: StatusIdle})
	dormant := prepareOne(t, Row{Status: StatusIdle, Dormant: true})

	assert.Equal(t, Cell{Glyph: GlyphQuiet, Tone: paint.ToneMuted}, quiet.Status)
	assert.True(t, dormant.Status.IsBlank())
	assert.NotEqual(t, quiet.Status, dormant.Status)
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
			name:   "notes without a conclusion read as unremarkable, the design spending no value on them",
			review: ReviewCommented,
			want:   Cell{Glyph: GlyphReview, Tone: paint.ToneMuted},
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

// TestPrepareWeight pins the structure a row carries in its lettering rather than in a glyph.
func TestPrepareWeight(t *testing.T) {
	tests := []struct {
		name     string
		kind     RowKind
		want     Weight
		wantRule bool
	}{
		{
			name:     "a child is italic and hangs off a corner",
			kind:     KindChild,
			want:     WeightItalic,
			wantRule: true,
		},
		{
			name:     "an ordinary agent is regular",
			kind:     KindAgent,
			want:     WeightRegular,
			wantRule: false,
		},
		{
			name:     "a workflow is bold",
			kind:     KindWorkflow,
			want:     WeightBold,
			wantRule: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := prepareOne(t, Row{Kind: tt.kind})

			assert.Equal(t, tt.want, line.Weight)
			assert.Equal(t, tt.wantRule, line.Rule)
		})
	}
}

// TestPrepareMarkKeepsItsOwnColumn pins that a letter in the gutter is a cell of its own rather
// than something that moves the name.
func TestPrepareMarkKeepsItsOwnColumn(t *testing.T) {
	tests := []struct {
		name string
		row  Row
		want Cell
	}{
		{
			name: "an unmarked row draws nothing in the gutter",
			row:  Row{},
			want: Cell{},
		},
		{
			name: "a marked row carries the letter and the mark tone",
			row:  Row{Mark: "a"},
			want: Cell{Text: "a", Tone: paint.ToneMark},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, prepareOne(t, tt.row).Mark)
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

	for _, cell := range []Cell{line.Mark, line.Status, line.Repo, line.Review, line.Forge, line.Held, line.Mail, line.Alert, line.Schedule} {
		assert.Equal(t, paint.ToneMuted, cell.Tone)
	}
}

// TestPrepareDimRowKeepsItsCaret pins the one cell greying leaves alone, since a hidden row the
// keyboard is standing on still has to say so.
func TestPrepareDimRowKeepsItsCaret(t *testing.T) {
	list := Prepare(Snapshot{
		Rows:    []Row{{Key: "hidden", Name: "hidden", Dim: true}},
		Focused: "hidden",
	})
	require.Len(t, list.Lines, 1)

	assert.Equal(t, Cell{Glyph: GlyphFocus, Tone: paint.ToneAccent}, list.Lines[0].Focus)
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

// TestPrepareFocus pins which row the keyboard is drawn as standing on.
func TestPrepareFocus(t *testing.T) {
	tests := []struct {
		name    string
		focused string
		want    []bool
	}{
		{
			name:    "a key belonging to no row focuses nothing",
			focused: "gone",
			want:    []bool{false, false, false},
		},
		{
			name:    "a snapshot naming no row focuses nothing rather than the first",
			focused: "",
			want:    []bool{false, false, false},
		},
		{
			name:    "the named row is focused and no other",
			focused: "two",
			want:    []bool{false, true, false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := Prepare(Snapshot{
				Rows:    []Row{{Key: "one", Name: "one"}, {Key: "two", Name: "two"}, {Key: "three", Name: "three"}},
				Focused: tt.focused,
			})
			require.Len(t, list.Lines, len(tt.want))

			for i, want := range tt.want {
				assert.Equal(t, want, list.Lines[i].Focused)
				assert.Equal(t, want, !list.Lines[i].Focus.IsBlank())
			}
		})
	}
}

// TestPrepareFocusSurvivesAReorder pins that naming the focused row rather than counting to it
// follows that row when the list is sorted after it was gathered.
func TestPrepareFocusSurvivesAReorder(t *testing.T) {
	gathered := []Row{{Key: "one", Name: "one"}, {Key: "two", Name: "two"}}
	sorted := []Row{gathered[1], gathered[0]}

	list := Prepare(Snapshot{Rows: sorted, Focused: "two"})
	require.Len(t, list.Lines, 2)

	assert.True(t, list.Lines[0].Focused)
	assert.False(t, list.Lines[1].Focused)
}

// TestPrepareClipsName pins the column a name is cut to.
func TestPrepareClipsName(t *testing.T) {
	tests := []struct {
		name  string
		given string
		pane  int
		want  string
	}{
		{
			name:  "a name past the column ends in an ellipsis",
			given: "eng-6994-unable-to-create",
			pane:  27,
			want:  "eng-6994-un…",
		},
		{
			name:  "a name inside the column is left alone",
			given: "iss-582",
			pane:  27,
			want:  "iss-582",
		},
		{
			name:  "no pane at all leaves the name whole rather than cutting it to nothing",
			given: "research-identity-assets",
			want:  "research-identity-assets",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := Prepare(Snapshot{Rows: []Row{{Name: tt.given}}, Pane: tt.pane})
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
	assert.Equal(t, "a", line.Mark.Text)
	assert.True(t, line.Selected)
}

// TestPrepareFocusOutranksEveryOtherEmphasis pins the row under the cursor being drawn as the
// row under the cursor whatever else it has to say.
func TestPrepareFocusOutranksEveryOtherEmphasis(t *testing.T) {
	tests := []struct {
		name string
		row  Row
		want Emphasis
	}{
		{
			name: "a hidden row under the cursor is still the row under the cursor",
			row:  Row{Key: "one", Dim: true},
			want: EmphasisFocused,
		},
		{
			name: "so is one nobody has read",
			row:  Row{Key: "one", Unviewed: true},
			want: EmphasisFocused,
		},
		{
			name: "an ordinary row under the cursor",
			row:  Row{Key: "one"},
			want: EmphasisFocused,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := Prepare(Snapshot{Rows: []Row{tt.row}, Focused: "one"})
			require.Len(t, list.Lines, 1)

			assert.Equal(t, tt.want, list.Lines[0].Emphasis)
		})
	}
}

// TestPrepareHiddenCaretKeepsTheRowFocused pins the blink taking the caret off without taking
// the focus with it, so a row stays lit while the caret is dark.
func TestPrepareHiddenCaretKeepsTheRowFocused(t *testing.T) {
	list := Prepare(Snapshot{Rows: []Row{{Key: "one", Name: "one"}}, Focused: "one", CaretHidden: true})
	require.Len(t, list.Lines, 1)

	assert.True(t, list.Lines[0].Focus.IsBlank())
	assert.True(t, list.Lines[0].Focused)
	assert.Equal(t, EmphasisFocused, list.Lines[0].Emphasis)
}

// TestPrepareClipsMatchesWithTheName pins a search match surviving the cut that shortens the
// name it sits in, since a match hanging off the end would be drawn over whatever follows.
func TestPrepareClipsMatchesWithTheName(t *testing.T) {
	tests := []struct {
		name    string
		matches []Span
		pane    int
		want    []Span
	}{
		{
			name:    "a match wholly past the cut is dropped",
			matches: []Span{{Start: 10, End: 14}},
			pane:    12,
			want:    nil,
		},
		{
			name:    "a match the cut runs through is shortened to what is left",
			matches: []Span{{Start: 3, End: 14}},
			pane:    12,
			want:    []Span{{Start: 3, End: 5}},
		},
		{
			name:    "a match before the cut is left alone",
			matches: []Span{{Start: 0, End: 4}},
			pane:    12,
			want:    []Span{{Start: 0, End: 4}},
		},
		{
			name:    "no cut at all leaves every match alone",
			matches: []Span{{Start: 10, End: 14}},
			want:    []Span{{Start: 10, End: 14}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := Row{Name: "agent-with-a-long-name", Matches: tt.matches}
			list := Prepare(Snapshot{Rows: []Row{row}, Pane: tt.pane})
			require.Len(t, list.Lines, 1)

			assert.Equal(t, tt.want, list.Lines[0].Matches)
		})
	}
}

// TestPrepareHeldCell pins the pause a person took, which is alarming where the agent is
// working through it.
func TestPrepareHeldCell(t *testing.T) {
	tests := []struct {
		name string
		row  Row
		want Cell
	}{
		{
			name: "a paused agent working anyway is drawn in danger",
			row:  Row{Held: true, HeldWorking: true},
			want: Cell{Glyph: GlyphPaused, Tone: paint.ToneDanger},
		},
		{
			name: "an ordinary pause is a warning",
			row:  Row{Held: true},
			want: Cell{Glyph: GlyphPaused, Tone: paint.ToneWarning},
		},
		{
			name: "an agent nobody paused draws nothing",
			row:  Row{},
			want: Cell{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, prepareOne(t, tt.row).Held)
		})
	}
}

// TestPrepareGivesTheTallyUpBeforeTheMarks pins what a row sheds when its pane is too narrow to
// hold everything: the name first, then the count of what is out of sight, never a mark.
func TestPrepareGivesTheTallyUpBeforeTheMarks(t *testing.T) {
	busiest := Row{
		Kind:        KindChild,
		Name:        "an agent with a very long name indeed",
		Held:        true,
		UnreadMail:  true,
		FailedRun:   true,
		Scheduled:   true,
		HiddenCount: 12,
	}

	tests := []struct {
		name      string
		pane      int
		wantName  string
		wantTally int
	}{
		{
			name:      "a pane that has already given a column up spends what is left on the name",
			pane:      28,
			wantName:  "a…",
			wantTally: 0,
		},
		{
			name:      "the default sidebar keeps all three columns and gives the tally up",
			pane:      30,
			wantName:  "…",
			wantTally: 0,
		},
		{
			name:      "a pane with room for both keeps the tally",
			pane:      60,
			wantName:  "an agent with a very long …",
			wantTally: 12,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := Prepare(Snapshot{Rows: []Row{busiest}, Pane: tt.pane})
			require.Len(t, list.Lines, 1)

			assert.Equal(t, tt.wantName, list.Lines[0].Name)
			assert.Equal(t, tt.wantTally, list.Lines[0].Tally)
			assert.False(t, list.Lines[0].Schedule.IsBlank(), "the marks are never what gives way")
		})
	}
}
