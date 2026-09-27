package view

import (
	"strings"
	"testing"

	"komrad/internal/ui/paint"

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
			name:   "a thinking agent gets the bright bar",
			status: StatusThinking,
			want:   Cell{Glyph: GlyphBusy, Tone: paint.ToneBright},
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

// TestPrepareBarSaysWhatStandsBehindARow pins the bar drawing on every row whatever became of
// its process, so the rail runs unbroken down the list.
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
			name: "a process that has stopped keeps the quiet pipe",
			row:  Row{Status: StatusIdle, Process: ProcessStopped},
			want: Cell{Glyph: GlyphQuiet, Tone: paint.ToneMuted},
		},
		{
			name: "a process that stopped mid-thought keeps the bar it stopped in",
			row:  Row{Status: StatusThinking, Process: ProcessStopped},
			want: Cell{Glyph: GlyphBusy, Tone: paint.ToneBright},
		},
		{
			name: "a row that never had a process draws the quiet pipe all the same",
			row:  Row{Status: StatusIdle, Process: ProcessNone},
			want: Cell{Glyph: GlyphQuiet, Tone: paint.ToneMuted},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, prepareOne(t, tt.row).Status)
		})
	}
}

// TestPrepareDimMeansTheProcessIsGone pins the one thing that greys a row: its process has
// stopped or it has been archived, and nothing a person did to it counts.
func TestPrepareDimMeansTheProcessIsGone(t *testing.T) {
	tests := []struct {
		name string
		row  Row
		want Emphasis
	}{
		{
			name: "an agent whose process has stopped is dim",
			row:  Row{Process: ProcessStopped},
			want: EmphasisDim,
		},
		{
			name: "an archived agent is dim",
			row:  Row{Archived: true},
			want: EmphasisDim,
		},
		{
			name: "a hidden agent still running is bright",
			row:  Row{Hidden: true},
			want: EmphasisNormal,
		},
		{
			name: "a hidden agent whose process has stopped is dim",
			row:  Row{Hidden: true, Process: ProcessStopped},
			want: EmphasisDim,
		},
		{
			name: "an idle agent is alive and stays bright",
			row:  Row{Status: StatusIdle},
			want: EmphasisNormal,
		},
		{
			name: "a row with no process behind it is bright",
			row:  Row{Process: ProcessNone},
			want: EmphasisNormal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, prepareOne(t, tt.row).Emphasis)
		})
	}
}

// TestPrepareHiddenMark pins the mark a person's hiding a row leaves after its name.
func TestPrepareHiddenMark(t *testing.T) {
	tests := []struct {
		name string
		row  Row
		want Cell
	}{
		{
			name: "a hidden row carries the muted mark",
			row:  Row{Hidden: true},
			want: Cell{Glyph: GlyphHidden, Tone: paint.ToneMuted},
		},
		{
			name: "a hidden row whose process has gone keeps it",
			row:  Row{Hidden: true, Process: ProcessStopped},
			want: Cell{Glyph: GlyphHidden, Tone: paint.ToneMuted},
		},
		{
			name: "an archived row does not carry it",
			row:  Row{Archived: true},
			want: Cell{},
		},
		{
			name: "an ordinary row draws nothing",
			row:  Row{},
			want: Cell{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, prepareOne(t, tt.row).Hidden)
		})
	}
}

// TestPrepareColumnsFollowTheRowKind pins the difference between a stage that has not happened
// and one that never can: the columns beside an agent's own row, under a heading or in a flat
// list, are drawn greyed, and a row that is not an agent's own draws them blank.
func TestPrepareColumnsFollowTheRowKind(t *testing.T) {
	tests := []struct {
		name  string
		row   Row
		blank bool
	}{
		{
			name: "an agent's own row carries all of them",
			row: Row{
				Name:           "worker",
				Repo:           RepoGone,
				PersonComments: &Comments{Open: 1},
				Forge:          ForgeMerged,
			},
		},
		{
			name: "a result is an agent's own row with no tree over it",
			row: Row{
				Kind:           KindResult,
				Name:           "worker",
				Repo:           RepoGone,
				PersonComments: &Comments{Open: 1},
				Forge:          ForgeMerged,
			},
		},
		{
			name: "a child leaves them to the row above it",
			row: Row{
				Kind:           KindChild,
				Name:           "reviewer",
				Repo:           RepoGone,
				PersonComments: &Comments{Open: 1},
				Forge:          ForgeMerged,
			},
			blank: true,
		},
		{
			name: "a workflow header stands for none of them",
			row: Row{
				Kind:           KindWorkflow,
				Name:           "komrad",
				Repo:           RepoGone,
				PersonComments: &Comments{Open: 1},
				Forge:          ForgeMerged,
			},
			blank: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := prepareOne(t, tt.row)

			require.Len(t, line.Columns, 3)
			for _, cell := range line.Columns {
				assert.Equal(t, tt.blank, cell.IsBlank())
			}
		})
	}
}

// TestPrepareBlankColumnsKeepEveryOtherCell pins that emptying the three columns takes nothing
// else off a row.
func TestPrepareBlankColumnsKeepEveryOtherCell(t *testing.T) {
	row := Row{
		Name:       "reviewer",
		Status:     StatusThinking,
		Held:       true,
		UnreadMail: true,
		FailedRun:  true,
		Scheduled:  true,
	}
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
			assert.Equal(t, tt.want, prepareOne(t, Row{Repo: tt.repo}).Columns[0])
		})
	}
}

// TestPrepareCommentColumns pins the two count pairs, reviewer agents then the person, and that the
// gutter makes room for each only while some row has any.
func TestPrepareCommentColumns(t *testing.T) {
	muted := func(text string) Cell {
		return Cell{Text: text, Tone: paint.ToneMuted}
	}
	unreadDot := Cell{Text: "·", Tone: paint.ToneWarning}

	tests := []struct {
		name         string
		row          Row
		wantColumns  Columns
		wantReviewer Cell
		wantPerson   Cell
	}{
		{
			name:        "a row with no comments leaves the counts out of the gutter",
			row:         Row{Kind: KindAgent},
			wantColumns: WorkColumns(),
		},
		{
			name: "a child row draws no counts",
			row: Row{
				Kind:           KindChild,
				PersonComments: &Comments{Open: 1},
			},
			wantColumns: CommentedWorkColumns(0, 3, false),
		},
		{
			name: "a side nobody has comments on is left out of the gutter",
			row: Row{
				Kind:             KindAgent,
				ReviewerComments: &Comments{Resolved: 2},
			},
			wantColumns:  CommentedWorkColumns(3, 0, false),
			wantReviewer: Cell{Parts: []Cell{muted("0"), muted("|2")}},
		},
		{
			name: "the person's counts take no place for a dot while nothing is unread",
			row: Row{
				Kind:           KindAgent,
				PersonComments: &Comments{Resolved: 12},
			},
			wantColumns: CommentedWorkColumns(0, 4, false),
			wantPerson:  Cell{Parts: []Cell{muted("0"), muted("|12")}},
		},
		{
			name: "the person's open count stands out and an unread one earns the dot",
			row: Row{
				Kind: KindAgent,
				ReviewerComments: &Comments{
					Open:     3,
					Resolved: 1,
				},
				PersonComments: &Comments{
					Open:     2,
					Resolved: 4,
					Unread:   true,
				},
			},
			wantColumns:  CommentedWorkColumns(3, 3, true),
			wantReviewer: Cell{Parts: []Cell{muted("3"), muted("|1")}},
			wantPerson: Cell{Parts: []Cell{
				{
					Text: "2",
					Tone: paint.ToneWarning,
				},
				muted("|4"),
				unreadDot,
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := Prepare(Snapshot{Rows: []Row{tt.row}})
			require.Len(t, list.Lines, 1)
			require.Equal(t, tt.wantColumns, list.Columns)

			drawn := map[ColumnKind]Cell{}
			for i, column := range list.Columns {
				drawn[column.Kind] = list.Lines[0].Columns[i]
			}

			assert.Equal(t, tt.wantReviewer, drawn[ColumnReviewerComments])
			assert.Equal(t, tt.wantPerson, drawn[ColumnPersonComments])
		})
	}
}

// TestPrepareForgeColumn pins where a pull request opened from the branch stands, every state
// of it, with nothing drawn while nothing is known or nothing was opened.
func TestPrepareForgeColumn(t *testing.T) {
	tests := []struct {
		name  string
		forge Forge
		want  Cell
	}{
		{
			name:  "a repository not read yet draws nothing",
			forge: ForgeUnread,
			want:  Cell{},
		},
		{
			name:  "no pull request from the branch draws nothing either",
			forge: ForgeNone,
			want:  Cell{},
		},
		{
			name:  "an open pull request is the pull request glyph greyed",
			forge: ForgeOpen,
			want:  Cell{Glyph: GlyphForge, Tone: paint.ToneMuted},
		},
		{
			name:  "approved gets the check in success",
			forge: ForgeApproved,
			want:  Cell{Glyph: GlyphApproved, Tone: paint.ToneSuccess},
		},
		{
			name:  "changes requested is the pull request glyph in warning",
			forge: ForgeChangesRequested,
			want:  Cell{Glyph: GlyphForge, Tone: paint.ToneWarning},
		},
		{
			name:  "a draft has its own glyph greyed",
			forge: ForgeDraft,
			want:  Cell{Glyph: GlyphDraft, Tone: paint.ToneMuted},
		},
		{
			name:  "an approval waiting on the forge keeps the check glyph, muted",
			forge: ForgeApprovedWaiting,
			want:  Cell{Glyph: GlyphApproved, Tone: paint.ToneMuted},
		},
		{
			name:  "failing checks get the failing glyph in danger",
			forge: ForgeFailing,
			want:  Cell{Glyph: GlyphFailing, Tone: paint.ToneDanger},
		},
		{
			name:  "a conflict with the base gets the conflicting glyph in danger",
			forge: ForgeConflicting,
			want:  Cell{Glyph: GlyphConflicting, Tone: paint.ToneDanger},
		},
		{
			name:  "a merged change gets its own glyph and its own tone",
			forge: ForgeMerged,
			want:  Cell{Glyph: GlyphMerged, Tone: paint.ToneMerged},
		},
		{
			name:  "closed without merging has its own glyph in danger",
			forge: ForgeClosed,
			want:  Cell{Glyph: GlyphClosed, Tone: paint.ToneDanger},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, prepareOne(t, Row{Forge: tt.forge}).Columns[1])
		})
	}

	assert.Len(t, tests, int(ForgeEnd), "every state is pinned")
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
			want: Cell{Text: "a", Tone: paint.ToneMuted},
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
			name: "an archived row with unread mail stays dim, since dim is the stronger fact",
			row:  Row{Archived: true, UnreadMail: true},
			want: EmphasisDim,
		},
		{
			name: "an archived row is dim",
			row:  Row{Archived: true},
			want: EmphasisDim,
		},
		{
			name: "a row with unread mail is not brightened, since the envelope says it",
			row:  Row{UnreadMail: true},
			want: EmphasisNormal,
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

// TestPrepareDimRowKeepsEveryCell pins greying reaching the name and nothing else, so a dormant
// agent still says in colour where its comments and its pull request stand.
func TestPrepareDimRowKeepsEveryCell(t *testing.T) {
	row := Row{
		Key:    "gone",
		Status: StatusBroken,
		Repo:   RepoGone,
		Forge:  ForgeMerged,
		PersonComments: &Comments{
			Open:   1,
			Unread: true,
		},
		Held:       true,
		Hidden:     true,
		UnreadMail: true,
		FailedRun:  true,
		Scheduled:  true,
	}

	for _, dim := range []func(*Row){
		func(r *Row) { r.Archived = true },
		func(r *Row) { r.Process = ProcessStopped },
	} {
		dimmed := row
		dim(&dimmed)

		lit, greyed := prepareOne(t, row), prepareOne(t, dimmed)
		require.Equal(t, EmphasisDim, greyed.Emphasis)

		assert.Equal(t, lit.Mark, greyed.Mark)
		assert.Equal(t, lit.Columns, greyed.Columns)
		assert.Equal(t, lit.Held, greyed.Held)
		assert.Equal(t, lit.Hidden, greyed.Hidden)
		assert.Equal(t, lit.Mail, greyed.Mail)
		assert.Equal(t, lit.Alert, greyed.Alert)
		assert.Equal(t, lit.Schedule, greyed.Schedule)
	}
}

// TestPrepareDimRowKeepsItsCaret pins a greyed row the keyboard is standing on still saying so.
func TestPrepareDimRowKeepsItsCaret(t *testing.T) {
	list := Prepare(Snapshot{
		Rows: []Row{{
			Key:     "gone",
			Name:    "gone",
			Process: ProcessStopped,
		}},
		Focused: "gone",
	})
	require.Len(t, list.Lines, 1)

	assert.Equal(t, Cell{Glyph: GlyphFocus, Tone: paint.ToneAccent}, list.Lines[0].Focus)
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
			name: "unread mail is the muted envelope",
			row:  Row{UnreadMail: true},
			want: Line{Mail: Cell{Glyph: GlyphMail, Tone: paint.ToneMuted}},
		},
		{
			name: "a paused agent is the muted pause",
			row:  Row{Held: true},
			want: Line{Held: Cell{Glyph: GlyphPaused, Tone: paint.ToneMuted}},
		},
		{
			name: "a hidden agent is the muted closed eye",
			row:  Row{Hidden: true},
			want: Line{Hidden: Cell{Glyph: GlyphHidden, Tone: paint.ToneMuted}},
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
			assert.Equal(t, tt.want.Hidden, line.Hidden)
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
			want:  "eng-6994-unabl…",
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
	line := prepareOne(t, Row{
		Name:     "strip-demo",
		Mark:     "a",
		Selected: true,
	})

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
			name: "a greyed row under the cursor is still the row under the cursor",
			row:  Row{Key: "one", Process: ProcessStopped},
			want: EmphasisFocused,
		},
		{
			name: "so is one nobody has read",
			row:  Row{Key: "one", UnreadMail: true},
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

// TestPrepareCaretStates pins the three ways the focused row's arrow stands: drawn, out for the
// blink with the name still lit, and absent with the accent gone while the row stays focused.
func TestPrepareCaretStates(t *testing.T) {
	tests := []struct {
		name     string
		caret    Caret
		blank    bool
		emphasis Emphasis
	}{
		{
			name:     "absent takes the accent too",
			caret:    CaretAbsent,
			blank:    true,
			emphasis: EmphasisNormal,
		},
		{
			name:     "blinked out keeps the row lit",
			caret:    CaretBlinkedOut,
			blank:    true,
			emphasis: EmphasisFocused,
		},
		{
			name:     "drawn",
			caret:    CaretDrawn,
			blank:    false,
			emphasis: EmphasisFocused,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := Prepare(Snapshot{
				Rows:    []Row{{Key: "one", Name: "one"}},
				Focused: "one",
				Caret:   tt.caret,
			})
			require.Len(t, list.Lines, 1)

			assert.Equal(t, tt.blank, list.Lines[0].Focus.IsBlank())
			assert.True(t, list.Lines[0].Focused, "the row is the anchor whatever the caret")
			assert.Equal(t, tt.emphasis, list.Lines[0].Emphasis)
		})
	}
}

// TestPrepareClipsMatchesWithTheName pins a search match surviving the cut that shortens the name
// it sits in.
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
			pane:    19,
			want:    nil,
		},
		{
			name:    "a match the cut runs through is shortened to what is left",
			matches: []Span{{Start: 3, End: 14}},
			pane:    19,
			want:    []Span{{Start: 3, End: 6}},
		},
		{
			name:    "a match before the cut is left alone",
			matches: []Span{{Start: 0, End: 4}},
			pane:    19,
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

// TestPrepareHeldCell pins the pause a person took.
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
			name: "an ordinary pause is muted",
			row:  Row{Held: true},
			want: Cell{Glyph: GlyphPaused, Tone: paint.ToneMuted},
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
			pane:      15,
			wantName:  "an…",
			wantTally: 0,
		},
		{
			name:      "the default sidebar has room for both",
			pane:      28,
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
// the worst row a panel can hold: an agent a schedule launched, hidden, paused, with documents,
// unread mail, and a failed run nobody has read.
func TestPrepareShedsMarksOnlyWhereTheMarksAloneWillNotFit(t *testing.T) {
	worst := Row{
		Kind:       KindAgent,
		Name:       "nightly-bughunt-dispatcher",
		Scheduled:  true,
		Hidden:     true,
		Held:       true,
		Documents:  true,
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
			pane: 15,
			want: []Glyph{GlyphAlert},
		},
		{
			name: "a pane two wider keeps the mail as well",
			pane: 17,
			want: []Glyph{GlyphMail, GlyphAlert},
		},
		{
			name: "a pane one wider keeps the pause too",
			pane: 18,
			want: []Glyph{GlyphPaused, GlyphMail, GlyphAlert},
		},
		{
			name: "the next keeps the hiding, which goes before the pause",
			pane: 20,
			want: []Glyph{GlyphPaused, GlyphHidden, GlyphMail, GlyphAlert},
		},
		{
			name: "the next keeps the documents, which go before the hiding",
			pane: 22,
			want: []Glyph{GlyphPaused, GlyphHidden, GlyphDocument, GlyphMail, GlyphAlert},
		},
		{
			name: "wider still keeps every mark, the schedule last",
			pane: 24,
			want: []Glyph{GlyphPaused, GlyphHidden, GlyphDocument, GlyphMail, GlyphAlert, GlyphSchedule},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := Prepare(Snapshot{Rows: []Row{worst}, Pane: tt.pane})
			require.Len(t, list.Lines, 1)

			drawn := make([]Glyph, 0, 6)
			for _, cell := range []Cell{list.Lines[0].Held, list.Lines[0].Hidden, list.Lines[0].Document, list.Lines[0].Mail, list.Lines[0].Alert, list.Lines[0].Schedule} {
				if !cell.IsBlank() {
					drawn = append(drawn, cell.Glyph)
				}
			}

			assert.Equal(t, tt.want, drawn)
		})
	}
}

// TestPrepareDrawsTheColumnsAScreenDeclares pins the one thing a screen says about its columns
// rather than its rows: the gutter is what it named, and the agents list's where it named none.
func TestPrepareDrawsTheColumnsAScreenDeclares(t *testing.T) {
	tests := []struct {
		name    string
		columns Columns
		want    Columns
	}{
		{
			name: "a screen naming nothing draws the agents list's two",
			want: Columns{
				{Kind: ColumnRepo, Width: 1},
				{Kind: ColumnForge, Width: 1},
			},
		},
		{
			name:    "the archive declares none",
			columns: NoColumns(),
			want:    Columns{},
		},
		{
			name: "a screen may declare a text column of its own",
			columns: Columns{
				{Kind: ColumnText, Width: 4},
				{Kind: ColumnRepo, Width: 1},
			},
			want: Columns{
				{Kind: ColumnText, Width: 4},
				{Kind: ColumnRepo, Width: 1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := Prepare(Snapshot{
				Rows:    []Row{{Name: "iss-582"}},
				Pane:    200,
				Columns: tt.columns,
			})

			assert.Equal(t, tt.want, list.Columns)
		})
	}
}

// TestPrepareFillsATextColumnFromTheRow pins where a text column's cell comes from: the row's
// cell at the column's own position, blank where the row carries none, and nothing at all under
// a glyph column.
func TestPrepareFillsATextColumnFromTheRow(t *testing.T) {
	columns := Columns{
		{Kind: ColumnText, Width: 4},
		{Kind: ColumnRepo, Width: 1},
		{Kind: ColumnText, Width: 3},
	}
	size := Cell{Text: "49M", Tone: paint.ToneMuted}
	tests := []struct {
		name string
		row  Row
		want []Cell
	}{
		{
			name: "a row with no cells is blank in every text column",
			row:  Row{Name: "iss-582", Kind: KindWorkflow},
			want: []Cell{{}, {}, {}},
		},
		{
			name: "a row's cell under a glyph column is ignored",
			row: Row{
				Name:  "iss-582",
				Kind:  KindWorkflow,
				Cells: []Cell{size, size},
			},
			want: []Cell{size, {}, {}},
		},
		{
			name: "cells past the last column are ignored",
			row: Row{
				Name:  "iss-582",
				Kind:  KindWorkflow,
				Cells: []Cell{size, {}, size, size},
			},
			want: []Cell{size, {}, size},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := Prepare(Snapshot{
				Rows:    []Row{tt.row},
				Columns: columns,
			})

			assert.Equal(t, tt.want, list.Lines[0].Columns)
		})
	}
}

// TestUndeclaredColumnsBuyTheNameTheirRoom pins that a column not declared is given up in the
// width arithmetic too.
func TestUndeclaredColumnsBuyTheNameTheirRoom(t *testing.T) {
	const pane = 60

	every := Prepare(Snapshot{Rows: []Row{{Name: "iss-582"}}, Pane: pane})
	none := Prepare(Snapshot{
		Rows:    []Row{{Name: "iss-582"}},
		Pane:    pane,
		Columns: NoColumns(),
	})

	assert.Equal(t, 4, NameColumns(pane, none.Columns)-NameColumns(pane, every.Columns),
		"each of the two costs an Octicon and the space after it")
}

// drawnResultNameColumn is the column a result list opens its name at, counted off the drawing
// by hand rather than from the package's own arithmetic.
const drawnResultNameColumn = 6

// resultRows are the rows a list of search hits is made of.
func resultRows() []Row {
	return []Row{
		{
			Key:  "live",
			Kind: KindResult,
			Name: "billing",
		},
		{
			Key:     "live/3",
			Kind:    KindHit,
			Lead:    "agent #3",
			Name:    "the ledger is reconciled nightly",
			Matches: []Span{{Start: 4, End: 10}},
		},
		{
			Key:      "archived",
			Kind:     KindResult,
			Name:     "payouts",
			Archived: true,
			Process:  ProcessStopped,
		},
		{
			Key:      "archived/1",
			Kind:     KindHit,
			Lead:     "you #1",
			Name:     "ledger",
			Matches:  []Span{{Start: 0, End: 6}},
			Archived: true,
			Process:  ProcessStopped,
		},
	}
}

// drawResults is a result list with its styling taken off.
func drawResults(pane int) []string {
	list := Prepare(Snapshot{
		Rows:    resultRows(),
		Columns: NoColumns(),
		Focused: "live",
		Pane:    pane,
	})

	return strings.Split(ansiEscapes.ReplaceAllString(render(list, paint.Panel, 0), ""), "\n")
}

// TestPrepareResultsDeclareNoColumn pins what a list of results asks for: no column at all,
// whatever the pane.
func TestPrepareResultsDeclareNoColumn(t *testing.T) {
	wide := Prepare(Snapshot{
		Rows:    resultRows(),
		Columns: NoColumns(),
		Pane:    200,
	})

	assert.Empty(t, wide.Columns)
	assert.Equal(t, everyColumn(), Prepare(Snapshot{Rows: resultRows(), Pane: 200}).Columns,
		"the same pane draws all three when none is named")
}

// TestResultRowOpensAtTheGutter pins that a result is not a tree node: nothing is above it to be
// indented under, so its name opens where a workflow's would and it is lettered as an ordinary
// row.
func TestResultRowOpensAtTheGutter(t *testing.T) {
	list := Prepare(Snapshot{
		Rows:    resultRows(),
		Columns: NoColumns(),
		Pane:    canonicalPane,
	})
	drawn := drawResults(canonicalPane)
	require.Len(t, drawn, len(list.Lines))

	for i, line := range list.Lines {
		if line.Lead != "" {
			continue
		}

		assert.Equal(t, 0, line.Indent, "row %q", drawn[i])
		assert.Equal(t, WeightRegular, line.Weight, "row %q", drawn[i])
		assert.Equal(t, drawnResultNameColumn, nameColumnOf(drawn[i], line), "row %q", drawn[i])
	}
}

// TestHitRowHangsUnderItsResult pins that a turn is drawn one step in under the agent that
// said it, its lead before the words.
func TestHitRowHangsUnderItsResult(t *testing.T) {
	list := Prepare(Snapshot{
		Rows:    resultRows(),
		Columns: NoColumns(),
		Pane:    canonicalPane,
	})
	drawn := drawResults(canonicalPane)
	require.Len(t, drawn, len(list.Lines))

	hits := 0
	for i, line := range list.Lines {
		if line.Lead == "" {
			continue
		}
		hits++

		assert.Equal(t, indentColumns, line.Indent, "row %q", drawn[i])
		assert.Equal(t, WeightRegular, line.Weight, "row %q", drawn[i])
		assert.False(t, line.Rule, "row %q", drawn[i])
		lead := strings.Index(drawn[i], line.Lead)
		require.GreaterOrEqual(t, lead, 0, "row %q", drawn[i])
		assert.Equal(t, wantNameColumn(KindHit), drawnColumns(drawn[i][:lead]), "row %q", drawn[i])
		assert.Equal(t, wantNameColumn(KindHit)+len(line.Lead)+1, nameColumnOf(drawn[i], line), "row %q", drawn[i])
		assert.Empty(t, line.Columns, "row %q", drawn[i])
	}

	assert.Equal(t, 2, hits)
}

// TestHitRowGivesItsLeadRoom pins that a narrow pane clips the words rather than the lead.
func TestHitRowGivesItsLeadRoom(t *testing.T) {
	line := prepareOne(t, Row{
		Kind: KindHit,
		Lead: "agent #3",
		Name: "the ledger is reconciled nightly",
	})
	fitted := fitRow(line, 20)

	assert.Equal(t, "agent #3", fitted.Lead)
	assert.Len(t, []rune(fitted.Name), 20-indentColumns-len("agent #3")-1)
}
