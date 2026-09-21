package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"weave/internal/ui/paint"
	"weave/lib/xslice"
	worktreev1 "weave/proto/gen/worktree/v1"
	"weave/proto/gen/worktree/v1/worktreev1connect"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readWorktree is a directory read through git as the tests hand it to the record.
func readWorktree(now time.Time) worktreeInspection {
	return worktreeInspection{
		answered: true,
		readAt:   now,
		item: worktreeItem{
			dir:       "/repo",
			readable:  true,
			branch:    "agent/x",
			occupants: []worktreeAgent{{id: "a2", name: "reader"}},
			size:      &worktreeSize{bytes: 1200 * 1024 * 1024, measuredAt: now},
		},
		modified:   []string{"a.go", "b.go"},
		untracked:  []string{"notes.md"},
		baseRef:    "origin/main",
		ahead:      3,
		behind:     12,
		landed:     1,
		sequencing: true,
		commits: []worktreeCommit{
			{sha: "87a3bcc98c47", subject: "fix(view): drop the column", committedAt: now.Add(-2 * time.Hour)},
			{sha: "1234567890ab", subject: "feat(ui): a thing", committedAt: now.Add(-3 * time.Hour)},
		},
	}
}

// TestAgentRecordDrawsItsWorktree covers the record under an agent carrying what get_worktree
// answered for its directory: where the branch stands and what is uncommitted after the branch,
// and the paths and commits themselves at the end.
func TestAgentRecordDrawsItsWorktree(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	now := time.Now()
	m.applyWorktreeInspection(worktreeInspectedMsg{dir: "/repo", inspection: readWorktree(now)})

	held := m.hoveredDetails()
	labels := fieldLabels(held)

	assert.Equal(t, "origin/main  ↑3 ↓12", fieldValue(t, held, "base"))
	assert.Equal(t, "2 modified, 1 untracked", fieldValue(t, held, "changes"))
	assert.Equal(t, "1 commit the base already holds", fieldValue(t, held, "landed"))
	assert.Equal(t, "a merge or rebase is part-way through", fieldValue(t, held, "sequence"))
	assert.Equal(t, "1.2G, measured 0s ago", fieldValue(t, held, "size"))
	assert.Equal(t, "reader", fieldValue(t, held, "also here"))
	assert.Less(t, indexOf(labels, "directory"), indexOf(labels, "base"), "the standing follows what was launched")
	assert.Less(t, indexOf(labels, "size"), indexOf(labels, "flags"), "and comes before the ids")

	assert.Equal(t, "M a.go", fieldValue(t, held, "uncommitted"))
	assert.Equal(t, "87a3bcc98c47  2h   fix(view): drop the column", fieldValue(t, held, "commits"))
	assert.Greater(t, indexOf(labels, "uncommitted"), indexOf(labels, "pr"), "the lists come after the metadata")

	trailing := held.fields[indexOf(labels, "uncommitted")+1]
	assert.Empty(t, trailing.label, "a second path carries no label of its own")
	assert.Equal(t, "M b.go", trailing.value)
}

// TestAgentRecordOnADirectoryThatIsNoWorktree covers the two answers that draw no worktree rows
// at all: nothing asked yet, and the service saying the directory is not one it holds.
func TestAgentRecordOnADirectoryThatIsNoWorktree(t *testing.T) {
	m := detailsModel(&recordingTmux{})

	assert.NotContains(t, fieldLabels(m.hoveredDetails()), "size", "nothing has been read")

	refused := connect.NewError(connect.CodeNotFound, errors.New("/repo is not under the worktree directories"))
	m.applyWorktreeInspection(worktreeInspectedMsg{dir: "/repo", inspection: worktreeInspection{answered: true, err: refused}})

	assert.NotContains(t, fieldLabels(m.hoveredDetails()), "worktree", "a directory weave does not hold is not an error")
	assert.NotContains(t, fieldLabels(m.hoveredDetails()), "size")
}

// TestAgentRecordSaysWhyTheWorktreeCouldNotBeRead covers git refusing a directory weave does
// hold, which the record says rather than drawing nothing.
func TestAgentRecordSaysWhyTheWorktreeCouldNotBeRead(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.applyWorktreeInspection(worktreeInspectedMsg{dir: "/repo", inspection: worktreeInspection{answered: true, err: errors.New("git exploded")}})

	assert.Equal(t, "could not read: git exploded", fieldValue(t, m.hoveredDetails(), "worktree"))
}

// TestAgentRecordOnAnUnreadableWorktree covers a registration git cannot read, which has a size
// but no standing to draw.
func TestAgentRecordOnAnUnreadableWorktree(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	read := readWorktree(time.Now())
	read.item.readable = false
	m.applyWorktreeInspection(worktreeInspectedMsg{dir: "/repo", inspection: read})

	labels := fieldLabels(m.hoveredDetails())

	assert.Contains(t, labels, "size")
	assert.NotContains(t, labels, "base")
	assert.NotContains(t, labels, "commits")
}

// TestWorktreeHistoryFieldsCutTheLists covers the paths and the commits past the first ten
// folding into one row that counts the rest.
func TestWorktreeHistoryFieldsCutTheLists(t *testing.T) {
	read := worktreeInspection{answered: true, item: worktreeItem{readable: true}, baseRef: "origin/main"}
	for i := range 12 {
		read.untracked = append(read.untracked, fmt.Sprintf("file%d", i))
		read.commits = append(read.commits, worktreeCommit{sha: fmt.Sprintf("%012d", i), subject: "a commit"})
	}

	fields := worktreeHistoryFields(read)

	require.Len(t, fields, 22)
	assert.Equal(t, "+ 2 more", fields[10].value)
	assert.Equal(t, "commits", fields[11].label)
	assert.Equal(t, "+ 2 more", fields[21].value)
}

// recordingWorktreeReads answers get_worktree and counts the directories asked for.
type recordingWorktreeReads struct {
	worktreev1connect.WorktreeServiceClient
	asked []string
}

func (r *recordingWorktreeReads) GetWorktree(_ context.Context, req *connect.Request[worktreev1.GetWorktreeRequest]) (*connect.Response[worktreev1.GetWorktreeResponse], error) {
	r.asked = append(r.asked, req.Msg.GetDir())

	return connect.NewResponse(&worktreev1.GetWorktreeResponse{
		Worktree:  &worktreev1.Worktree{Dir: req.Msg.GetDir(), Readable: true, Branch: "agent/x"},
		SizeBytes: 2048,
		Modified:  []string{"a.go"},
		BaseRef:   "origin/main",
		Ahead:     1,
	}), nil
}

// TestStageTickReadsTheHoveredAgentsWorktree covers the rest of the cursor being what sends
// get_worktree for the agent's directory, the answer landing on the record, a fresh answer not
// being asked for again, and a stale one being.
func TestStageTickReadsTheHoveredAgentsWorktree(t *testing.T) {
	client := &recordingWorktreeReads{}
	m := gitModel(t)
	m.api.worktrees = client
	m.api.pullRequests = &recordingPullRequests{}

	next, _ := m.deferStaging()
	ask := next.applyStageTick(stageTickMsg{move: next.staging})
	require.NotNil(t, ask)
	assert.True(t, next.inspections["/repo"].pending)

	for _, msg := range produced(ask) {
		updated, _ := next.Update(msg)
		landed, ok := updated.(model)
		require.True(t, ok)
		next = landed
	}

	assert.Equal(t, []string{"/repo"}, client.asked)
	read := next.inspections["/repo"]
	assert.True(t, read.answered)
	assert.False(t, read.pending)
	assert.Equal(t, "origin/main  ↑1", fieldValue(t, next.hoveredDetails(), "base"))

	assert.Nil(t, next.inspectAgentWorktreeCmd(time.Now()), "a fresh answer is not asked for again")
	assert.NotNil(t, next.inspectAgentWorktreeCmd(time.Now().Add(worktreeReadTTL+time.Second)), "a stale one is")
}

// TestFocusingTheRecordReadsTheWorktreeAgain covers the focus key asking for the directory
// whatever stands for it, so what the record expands to show is fresh.
func TestFocusingTheRecordReadsTheWorktreeAgain(t *testing.T) {
	client := &recordingWorktreeReads{}
	m := gitModel(t)
	m.api.worktrees = client
	m.applyWorktreeInspection(worktreeInspectedMsg{dir: "/repo", inspection: readWorktree(time.Now())})

	focused, cmd := m.toggleDetailsFocus()
	require.True(t, focused.details.focused)
	require.NotNil(t, cmd)

	produced(cmd)

	assert.Equal(t, []string{"/repo"}, client.asked)
}

// TestFocusedRecordGrowsToShowEverything covers the record taking the keys, when it grows from
// its share to every field it holds, keeping a row for the list, and shrinks back on the way out.
func TestFocusedRecordGrowsToShowEverything(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 40, 60
	m.applyWorktreeInspection(worktreeInspectedMsg{dir: "/repo", inspection: readWorktree(time.Now())})

	share := m.detailsLines()
	require.Greater(t, len(m.hoveredDetails().fields), share, "the record must not fit its share for this to prove anything")
	assert.NotContains(t, ansi.Strip(m.detailsView()), "commits", "the last fields are past the share")

	focused := send(t, m, "ctrl+t")
	require.True(t, focused.isDetailsFocused())

	assert.Equal(t, len(focused.hoveredDetails().fields), focused.grownDetailsLines(focused.hoveredDetails()))
	drawn := ansi.Strip(focused.detailsView())
	assert.Contains(t, drawn, "commits")
	assert.NotContains(t, drawn, "more", "nothing is left out")
	assert.Len(t, lines(focused.View()), focused.height)
	assert.Contains(t, outsideTheRecord(focused), "auth", "the list keeps its hovered row")

	back := send(t, focused, "ctrl+t")

	assert.Equal(t, share+detailsChromeLines, strings.Count(back.detailsView(), "\n"))
}

// TestFocusedRecordStopsAtTheRoom covers a record taller than the pane, which grows only as far
// as the pane allows and windows the rest.
func TestFocusedRecordStopsAtTheRoom(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 24, 60
	m.applyWorktreeInspection(worktreeInspectedMsg{dir: "/repo", inspection: readWorktree(time.Now())})

	focused := send(t, m, "ctrl+t")
	require.True(t, focused.isDetailsFocused())

	assert.Less(t, focused.grownDetailsLines(focused.hoveredDetails()), len(focused.hoveredDetails().fields))
	assert.Len(t, lines(focused.View()), focused.height)
	assert.Contains(t, ansi.Strip(focused.detailsView()), "more")
}

func fieldLabels(held details) []string {
	labels := xslice.Map(held.fields, func(field recordField) string {
		return field.label
	})

	return labels
}

// recordSearchModel is the record focused over an agent whose directory has been read.
func recordSearchModel(t *testing.T) model {
	t.Helper()

	m := detailsModel(&recordingTmux{})
	m.height, m.width = 40, 60
	m.applyWorktreeInspection(worktreeInspectedMsg{dir: "/repo", inspection: readWorktree(time.Now())})

	focused := send(t, m, "ctrl+t")
	require.True(t, focused.isDetailsFocused())

	return focused
}

// TestRecordSearchWalksOntoTheMatch covers the slash over a focused record: typing moves the
// field cursor onto the nearest match, the list's own search stands untouched, and enter keeps
// the search while putting the input away.
func TestRecordSearchWalksOntoTheMatch(t *testing.T) {
	m := recordSearchModel(t)

	opened := send(t, m, "/")
	require.True(t, opened.details.filter.typing)
	assert.False(t, opened.filter.typing, "the list's search is not the one opened")

	typed := pressMark(t, opened, "s", "i", "z")
	held := typed.hoveredDetails()
	assert.Equal(t, "size", held.fields[typed.detailsCursor(held)].label)
	assert.Contains(t, ansi.Strip(typed.View()), "/ siz", "the header's search line is the record's")

	kept := pressMark(t, typed, "enter")
	assert.False(t, kept.details.filter.typing)
	assert.True(t, kept.details.filter.isStanding())
	assert.False(t, kept.filter.isStanding(), "the list's search stands untouched")
}

// TestRecordSearchStepsBetweenMatches covers n and N walking the field cursor between the rows
// a standing search matches, and only once one stands.
func TestRecordSearchStepsBetweenMatches(t *testing.T) {
	m := recordSearchModel(t)

	unmoved := send(t, m, "n")
	assert.Equal(t, 0, unmoved.detailsCursor(unmoved.hoveredDetails()))

	standing := pressMark(t, send(t, m, "/"), "c", "o", "m", "m", "i", "t", "enter")
	held := standing.hoveredDetails()
	first := standing.detailsCursor(held)
	assert.Equal(t, "landed", held.fields[first].label)

	next := send(t, standing, "n")
	assert.Equal(t, "uncommitted", held.fields[next.detailsCursor(held)].label)

	back := send(t, next, "N")
	assert.Equal(t, first, back.detailsCursor(held))
}

// TestRecordFilterNarrowsTheFields covers tab flipping the search into a filter, which takes the
// fields it does not match off the record while a row under a matched label stays with its
// group, and the cursor and the copy key following the rows that are left.
func TestRecordFilterNarrowsTheFields(t *testing.T) {
	m := recordSearchModel(t)
	clip := &fakeClipboard{}
	m.clip = clip

	narrowed := pressMark(t, send(t, m, "/"), "c", "o", "m", "m", "i", "t", "tab", "enter")
	require.True(t, narrowed.details.filter.isNarrowing())

	rows := narrowed.detailsRows(narrowed.hoveredDetails())
	require.Len(t, rows, 6)
	assert.Equal(t, "landed", rows[0].field.label)
	assert.Equal(t, "M b.go", rows[2].field.value, "a path under the uncommitted label stays with it")
	assert.Equal(t, "uncommitted ? notes.md", rows[3].text)
	assert.Contains(t, ansi.Strip(narrowed.View()), "▽ commit")
	assert.NotContains(t, ansi.Strip(narrowed.detailsView()), "directory")

	bottom := send(t, narrowed, "G")
	copied, cmd := bottom.copyDetailsField()
	require.NotNil(t, cmd)
	_, ok := copied.(model)
	require.True(t, ok)
	produced(cmd)

	assert.Equal(t, []string{"1234567890ab  3h   feat(ui): a thing"}, clip.written)
}

// TestRecordFilterKeepsTheCountedRest covers the "+ N more" row of a cut list staying with its
// group under a filter, so a narrowed record still says what it left out.
func TestRecordFilterKeepsTheCountedRest(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 40, 60
	read := readWorktree(time.Now())
	for i := range 12 {
		read.untracked = append(read.untracked, fmt.Sprintf("file%d", i))
	}
	m.applyWorktreeInspection(worktreeInspectedMsg{dir: "/repo", inspection: read})

	narrowed := pressMark(t, send(t, m, "ctrl+t"), "/", "u", "n", "c", "o", "m", "tab", "enter")

	rows := narrowed.detailsRows(narrowed.hoveredDetails())
	require.Len(t, rows, worktreeRowsShown+1)
	assert.Equal(t, "+ 5 more", rows[worktreeRowsShown].field.value)
}

// TestRecordFilterLeavingNothingSaysSo covers a filter no field matches, where the record says
// so rather than drawing an empty block.
func TestRecordFilterLeavingNothingSaysSo(t *testing.T) {
	m := recordSearchModel(t)

	narrowed := pressMark(t, send(t, m, "/"), "z", "z", "z", "tab")

	assert.Contains(t, ansi.Strip(narrowed.detailsView()), "nothing matches")
	assert.True(t, narrowed.isDetailsFocused(), "the record keeps the keys")
}

// TestRecordSearchEscapes covers escape over the record: it abandons an input being typed at,
// clears a standing search before it hands the keys back, and hands them back only once nothing
// stands.
func TestRecordSearchEscapes(t *testing.T) {
	m := recordSearchModel(t)

	typing := pressMark(t, send(t, m, "/"), "s", "i", "z")
	abandoned := pressMark(t, typing, "esc")
	assert.False(t, abandoned.details.filter.isStanding())
	assert.True(t, abandoned.isDetailsFocused(), "abandoning the input keeps the keys on the record")
	assert.Equal(t, 0, abandoned.detailsCursor(abandoned.hoveredDetails()), "and the cursor is back where it started")

	standing := pressMark(t, send(t, m, "/"), "s", "i", "z", "enter")
	cleared := send(t, standing, "esc")
	assert.False(t, cleared.details.filter.isStanding(), "the first escape clears the search")
	assert.True(t, cleared.isDetailsFocused())

	back := send(t, cleared, "esc")
	assert.False(t, back.isDetailsFocused(), "the second hands the keys back")
}

// TestRecordSearchGoesWithTheKeys covers the focus key handing the keys back, which takes the
// record's standing search with it so the record is whole again.
func TestRecordSearchGoesWithTheKeys(t *testing.T) {
	m := recordSearchModel(t)

	standing := pressMark(t, send(t, m, "/"), "s", "i", "z", "tab", "enter")
	require.Len(t, standing.detailsRows(standing.hoveredDetails()), 1)

	back := send(t, standing, "ctrl+t")

	require.False(t, back.isDetailsFocused())
	assert.False(t, back.details.filter.isStanding())
	assert.Len(t, back.detailsRows(back.hoveredDetails()), len(back.hoveredDetails().fields))
}

// TestRecordSearchDroppedLeavingScreen covers the record's search going with the list's when
// the screen changes under it.
func TestRecordSearchDroppedLeavingScreen(t *testing.T) {
	m := recordSearchModel(t)

	standing := pressMark(t, send(t, m, "/"), "s", "i", "z", "enter")
	require.True(t, standing.details.filter.isStanding())

	standing.enterScreen(screenSchedules)

	assert.False(t, standing.details.filter.isStanding())
}

// TestRecordSearchLightsTheMatch covers the matched run of a value taking the match colour,
// which the row is drawn in pieces to carry, and the pieces still adding up to the value.
func TestRecordSearchLightsTheMatch(t *testing.T) {
	restore := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })

	m := recordSearchModel(t)
	standing := pressMark(t, send(t, m, "/"), "o", "r", "i", "g", "i", "n", "enter")

	held := standing.hoveredDetails()
	rows := standing.detailFieldRows(standing.detailsRows(held), standing.detailsCursor(held))
	base := rows[indexOf(fieldLabels(held), "base")]

	assert.Contains(t, ansi.Strip(base), "origin/main  ↑3 ↓12")
	assert.Contains(t, base, paint.Panel.Match().Render("origin"), "the match is lit")
}

// TestFocusedRecordStaysThroughItsSearch covers the record keeping the keys while a search is
// typed over it at the sizes where its legend growing used to hide it, so the enter that keeps
// the search never reaches the list.
func TestFocusedRecordStaysThroughItsSearch(t *testing.T) {
	tests := []struct {
		name          string
		width, height int
		expanded      bool
	}{
		{name: "a pane the share only just fits", width: 80, height: 13},
		{name: "one row taller", width: 80, height: 14},
		{name: "a narrow pane with the legend expanded", width: 30, height: 24, expanded: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := screensModel()
			m.width, m.height, m.helpExpanded = tt.width, tt.height, tt.expanded
			m.applyWorktreeInspection(worktreeInspectedMsg{dir: "", inspection: readWorktree(time.Now())})

			focused := send(t, m, "ctrl+t")
			require.True(t, focused.isDetailsFocused(), "the share fits, so the record takes the keys")

			typing := pressMark(t, focused, "/", "a")
			assert.True(t, typing.details.filter.typing)
			assert.True(t, typing.isDetailsFocused(), "the record is still drawn with its search line")
			assert.NotEmpty(t, typing.detailsView())
			assert.Len(t, lines(typing.View()), tt.height)

			kept := pressMark(t, typing, "enter")
			assert.True(t, kept.details.filter.isStanding())
			assert.True(t, kept.isDetailsFocused(), "enter kept the search rather than reaching the list")
			assert.Equal(t, screenAgents, kept.screen)

			back := pressMark(t, kept, "esc", "esc")
			assert.False(t, back.isDetailsFocused(), "two escapes clear the search and hand the keys back")
		})
	}
}

// failingWorktreeReads answers get_worktree once and refuses every read after it.
type failingWorktreeReads struct {
	recordingWorktreeReads
}

func (r *failingWorktreeReads) GetWorktree(ctx context.Context, req *connect.Request[worktreev1.GetWorktreeRequest]) (*connect.Response[worktreev1.GetWorktreeResponse], error) {
	if len(r.asked) > 0 {
		r.asked = append(r.asked, req.Msg.GetDir())

		return nil, connect.NewError(connect.CodeInternal, errors.New("context deadline exceeded"))
	}

	return r.recordingWorktreeReads.GetWorktree(ctx, req)
}

// TestFailedRereadKeepsTheLastAnswer covers a read that fails after one that answered, where
// the row keeps what it had and says the read was refused beside it.
func TestFailedRereadKeepsTheLastAnswer(t *testing.T) {
	client := &failingWorktreeReads{}
	m := gitModel(t)
	m.api.worktrees = client
	m.api.pullRequests = &recordingPullRequests{}

	m = landWorktreeRead(t, m, m.rereadAgentWorktreeCmd())
	require.Equal(t, "origin/main  ↑1", fieldValue(t, m.hoveredDetails(), "base"))

	m = landWorktreeRead(t, m, m.rereadAgentWorktreeCmd())
	require.Len(t, client.asked, 2)

	held := m.hoveredDetails()
	assert.Equal(t, "could not read: context deadline exceeded", fieldValue(t, held, "worktree"))
	assert.Equal(t, "origin/main  ↑1", fieldValue(t, held, "base"), "the last answer stays on the row")
	assert.Equal(t, "M a.go", fieldValue(t, held, "uncommitted"))
}

// TestNotFoundAnswerStandsForGood covers the service saying the directory is not a worktree it
// holds, which neither the cursor resting again nor the focus key asks about twice.
func TestNotFoundAnswerStandsForGood(t *testing.T) {
	m := gitModel(t)
	refused := connect.NewError(connect.CodeNotFound, errors.New("/repo is not under the worktree directories"))
	m.applyWorktreeInspection(worktreeInspectedMsg{dir: "/repo", inspection: worktreeInspection{answered: true, err: refused}})

	assert.Nil(t, m.inspectAgentWorktreeCmd(time.Now().Add(worktreeReadTTL+time.Hour)))
	assert.Nil(t, m.rereadAgentWorktreeCmd())
}

func landWorktreeRead(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()
	require.NotNil(t, cmd)

	for _, msg := range produced(cmd) {
		updated, _ := m.Update(msg)
		next, ok := updated.(model)
		require.True(t, ok)
		m = next
	}

	return m
}

// TestBlindTypingAfterAShrink covers the record losing its room to a resize while it held the
// keys, after which a slash opens the list's search on the header line rather than a search
// nobody can see.
func TestBlindTypingAfterAShrink(t *testing.T) {
	m := screensModel()
	m.width, m.height = 80, 30

	focused := send(t, m, "ctrl+t")
	require.True(t, focused.isDetailsFocused())

	shrunk := asModel(t, resize(focused, 80, 11))
	require.False(t, shrunk.isDetailsFocused())

	typed := pressMark(t, shrunk, "/", "a", "g", "e", "n", "t", "3")

	assert.True(t, typed.filter.typing, "the list's search took the keys")
	assert.Contains(t, ansi.Strip(typed.View()), "/ agent3", "and the header shows it")
}

// TestGrownRecordWithNoRowDrawsNothing covers a share so small that a focused record's rules
// alone would overrun the pane, where the record draws nothing rather than two bare rules.
func TestGrownRecordWithNoRowDrawsNothing(t *testing.T) {
	m := screensModel()
	m.width, m.height, m.detailsHeight, m.helpExpanded = 40, 14, 2, true

	focused := pressMark(t, send(t, m, "ctrl+t"), "/", "a", "tab", "enter")
	require.True(t, focused.details.focused)
	require.Equal(t, 0, focused.grownDetailsLines(focused.hoveredDetails()), "the corner this pins")

	assert.Empty(t, focused.detailsView())
	assert.Len(t, lines(focused.View()), focused.height)
}
