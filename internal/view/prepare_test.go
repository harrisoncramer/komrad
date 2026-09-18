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

// TestPrepareBarSaysWhatStandsBehindARow pins the four values the bar takes, blank among them
// meaning a row that never had a process rather than one whose process has gone.
func TestPrepareBarSaysWhatStandsBehindARow(t *testing.T) {
	tests := []struct {
		name string
		row  Row
		want Cell
	}{
		{
			name: "a process sitting quiet is a solid pipe",
			row:  Row{Status: StatusIdle},
			want: Cell{Glyph: GlyphQuiet, Tone: paint.ToneMuted},
		},
		{
			name: "a process that has stopped is a dashed pipe",
			row:  Row{Status: StatusIdle, Process: ProcessStopped},
			want: Cell{Glyph: GlyphStopped, Tone: paint.ToneMuted},
		},
		{
			name: "a process that stopped mid-thought is dashed rather than busy",
			row:  Row{Status: StatusThinking, Process: ProcessStopped},
			want: Cell{Glyph: GlyphStopped, Tone: paint.ToneMuted},
		},
		{
			name: "a row that never had a process draws nothing",
			row:  Row{Status: StatusIdle, Process: ProcessNone},
			want: Cell{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, prepareOne(t, tt.row).Status)
		})
	}
}

// TestPrepareBarTellsAStoppedProcessFromNone pins a stopped process drawing a mark where a row
// with no process at all draws none.
func TestPrepareBarTellsAStoppedProcessFromNone(t *testing.T) {
	stopped := prepareOne(t, Row{Process: ProcessStopped}).Status
	none := prepareOne(t, Row{Process: ProcessNone}).Status

	assert.False(t, stopped.IsBlank())
	assert.True(t, none.IsBlank())
	assert.NotEqual(t, stopped, none)
}

// TestPrepareColumnsFollowTheRowKind pins the difference between a stage that has not happened
// and one that never can: the three columns beside an agent's own row are drawn greyed, and a
// row that is not an agent's own draws them blank.
func TestPrepareColumnsFollowTheRowKind(t *testing.T) {
	tests := []struct {
		name  string
		row   Row
		blank bool
	}{
		{
			name: "an agent's own row carries all three",
			row:  Row{Name: "worker", Repo: RepoGone, Review: ReviewApproved, Forge: ForgeMerged},
		},
		{
			name:  "a child leaves them to the row above it",
			row:   Row{Kind: KindChild, Name: "reviewer", Repo: RepoGone, Review: ReviewApproved, Forge: ForgeMerged},
			blank: true,
		},
		{
			name:  "a workflow header stands for none of them",
			row:   Row{Kind: KindWorkflow, Name: "weave", Repo: RepoGone, Review: ReviewApproved, Forge: ForgeMerged},
			blank: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := prepareOne(t, tt.row)

			for _, cell := range []Cell{line.Repo, line.Review, line.Forge} {
				assert.Equal(t, tt.blank, cell.IsBlank())
			}
		})
	}
}

// TestPrepareBlankColumnsKeepEveryOtherCell pins that emptying the three columns takes nothing
// else off a row.
func TestPrepareBlankColumnsKeepEveryOtherCell(t *testing.T) {
	row := Row{Name: "reviewer", Status: StatusThinking, Held: true, UnreadMail: true, FailedRun: true, Scheduled: true}
	own := prepareOne(t, row)

	row.Kind = KindChild
	child := prepareOne(t, row)

	assert.Equal(t, own.Status, child.Status)
	assert.Equal(t, own.Held, child.Held)
	assert.Equal(t, own.Mail, child.Mail)
	assert.Equal(t, own.Alert, child.Alert)
	assert.Equal(t, own.Schedule, child.Schedule)
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

	dim := row
	dim.Dim = true

	lit, dimmed := prepareOne(t, row), prepareOne(t, dim)

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
			pane:  29,
			want:  "eng-6994-unabl…",
		},
		{
			name:  "a name inside the column is left alone",
			given: "iss-582",
			pane:  29,
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
			pane:    17,
			want:    nil,
		},
		{
			name:    "a match the cut runs through is shortened to what is left",
			matches: []Span{{Start: 3, End: 14}},
			pane:    17,
			want:    []Span{{Start: 3, End: 6}},
		},
		{
			name:    "a match before the cut is left alone",
			matches: []Span{{Start: 0, End: 4}},
			pane:    17,
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

// TestPrepareGivesTheTallyUpBeforeTheMarks pins what a row sheds as its pane narrows: the name
// first, then the count of what is out of sight, and never a mark while the marks still fit.
func TestPrepareGivesTheTallyUpBeforeTheMarks(t *testing.T) {
	// A workflow row is the one that carries a tally beside a schedule, a child never having
	// either.
	busiest := Row{
		Kind:        KindWorkflow,
		Name:        "an agent with a very long name indeed",
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
			name:      "a pane with nothing left for a name gives the tally up and spends what it frees",
			pane:      13,
			wantName:  "an…",
			wantTally: 0,
		},
		{
			name:      "the default sidebar has room for both",
			pane:      30,
			wantName:  "an agent wi…",
			wantTally: 12,
		},
		{
			name:      "a wide sidebar cuts nothing at all",
			pane:      60,
			wantName:  "an agent with a very long name indeed",
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

// TestPrepareShedsMarksOnlyWhereTheMarksAloneWillNotFit pins the last thing a row gives up, on
// the worst row a panel can hold: an agent a schedule launched, paused, with unread mail and a
// failed run nobody has read either.
func TestPrepareShedsMarksOnlyWhereTheMarksAloneWillNotFit(t *testing.T) {
	worst := Row{
		Kind:       KindAgent,
		Name:       "nightly-bughunt-dispatcher",
		Scheduled:  true,
		Held:       true,
		UnreadMail: true,
		FailedRun:  true,
	}

	tests := []struct {
		name string
		pane int
		want []Glyph
	}{
		{
			name: "a pane with room for one mark keeps the failed run alone",
			pane: 13,
			want: []Glyph{GlyphAlert},
		},
		{
			name: "a pane two wider keeps the mail as well",
			pane: 15,
			want: []Glyph{GlyphMail, GlyphAlert},
		},
		{
			name: "the width Harry runs keeps every mark but the schedule",
			pane: 16,
			want: []Glyph{GlyphPaused, GlyphMail, GlyphAlert},
		},
		{
			name: "a pane wide enough for the marks keeps every one of them",
			pane: 30,
			want: []Glyph{GlyphPaused, GlyphMail, GlyphAlert, GlyphSchedule},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := Prepare(Snapshot{Rows: []Row{worst}, Pane: tt.pane})
			require.Len(t, list.Lines, 1)

			drawn := make([]Glyph, 0, 4)
			for _, cell := range []Cell{list.Lines[0].Held, list.Lines[0].Mail, list.Lines[0].Alert, list.Lines[0].Schedule} {
				if !cell.IsBlank() {
					drawn = append(drawn, cell.Glyph)
				}
			}

			assert.Equal(t, tt.want, drawn)
		})
	}
}

// TestPrepareDropsColumnsAScreenDoesNotDraw pins the one thing a screen says about its columns
// rather than its rows: a column given up here is gone whatever the pane.
func TestPrepareDropsColumnsAScreenDoesNotDraw(t *testing.T) {
	tests := []struct {
		name    string
		dropped Columns
		want    Columns
	}{
		{
			name: "a screen dropping nothing draws all three",
			want: Columns{Repo: true, Review: true, Forge: true},
		},
		{
			name:    "the archive gives up all three",
			dropped: Columns{Repo: true, Review: true, Forge: true},
		},
		{
			name:    "a screen may give up one and keep the rest",
			dropped: Columns{Review: true},
			want:    Columns{Repo: true, Forge: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A pane far wider than the three columns need, so nothing here is the narrowing.
			list := Prepare(Snapshot{Rows: []Row{{Name: "iss-582"}}, Pane: 200, Dropped: tt.dropped})

			assert.Equal(t, tt.want, list.Columns)
		})
	}
}

// TestDroppedColumnsBuyTheNameTheirRoom pins that a column given up is given up in the width
// arithmetic too, rather than only in the drawing.
func TestDroppedColumnsBuyTheNameTheirRoom(t *testing.T) {
	const pane = 60

	every := Prepare(Snapshot{Rows: []Row{{Name: "iss-582"}}, Pane: pane})
	none := Prepare(Snapshot{
		Rows:    []Row{{Name: "iss-582"}},
		Pane:    pane,
		Dropped: Columns{Repo: true, Review: true, Forge: true},
	})

	assert.Equal(t, 6, NameColumns(pane, none.Columns)-NameColumns(pane, every.Columns),
		"each of the three costs an Octicon and the space after it")
}
