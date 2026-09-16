package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pressExpand presses the expand key and hands back the screen it left.
func pressExpand(t *testing.T, m model) model {
	t.Helper()

	next, _ := m.updateReview(keyMsg("E"))
	expanded, ok := next.(model)
	require.True(t, ok)

	return expanded
}

// tallReview is a review screen over a file with lines the patch does not draw.
func tallReview(t *testing.T) (model, *fakeReviewAgents) {
	t.Helper()

	agents := &fakeReviewAgents{}
	m, _ := reviewModelOver(t, agents, newTallFakeRepo(t.TempDir()))

	return openReview(t, m), agents
}

// TestExpandDrawsTheLinesThePatchLeftOut covers the whole point of the key: the file's own text
// above and below the hunk, which is where the reader has to go to judge whether the change is
// right.
func TestExpandDrawsTheLinesThePatchLeftOut(t *testing.T) {
	m, _ := tallReview(t)

	before := ansi.Strip(reviewFrameText(t, m))
	require.Contains(t, before, "func Two()", "the hunk is drawn to begin with")
	require.NotContains(t, before, "func Four()", "and the end of the file is not")

	after := ansi.Strip(reviewFrameText(t, pressExpand(t, m)))
	assert.Contains(t, after, "func Four()", "the lines below the hunk are drawn")
	assert.Contains(t, after, "import \"fmt\"", "and the lines above it")
}

// TestExpandGivesTheChangesBackAgain covers the key being a toggle, since a reader who opened a
// long file out needs the tight view back without leaving the screen.
func TestExpandGivesTheChangesBackAgain(t *testing.T) {
	m, _ := tallReview(t)

	expanded := pressExpand(t, m)
	require.Contains(t, ansi.Strip(reviewFrameText(t, expanded)), "func Four()")

	tight := ansi.Strip(reviewFrameText(t, pressExpand(t, expanded)))
	assert.NotContains(t, tight, "func Four()", "the file is drawn as its hunks again")
	assert.Contains(t, tight, "func Two()", "and the change is still there")
}

// TestExpandKeepsTheCursorOnItsLine covers the rows multiplying under a reader who is standing on
// one of them: the line they were reading has to stay where they were reading it.
func TestExpandKeepsTheCursorOnItsLine(t *testing.T) {
	m, _ := tallReview(t)

	m.review.cursor = rowOfNewLine(t, m, 7)
	before, ok := m.review.view.GetPlace(m.review.cursor)
	require.True(t, ok)

	expanded := pressExpand(t, m)

	after, ok := expanded.review.view.GetPlace(expanded.review.cursor)
	require.True(t, ok)
	assert.Equal(t, before, after, "the cursor is standing on the line it was standing on")
}

// TestExpansionSurvivesCatchingUp covers a reload, which is what happens when the reader catches
// up with the agent: losing the file they had opened out on every catch-up would make the key not
// worth pressing on a change they had to think about.
func TestExpansionSurvivesCatchingUp(t *testing.T) {
	m, _ := tallReview(t)

	expanded := pressExpand(t, m)
	require.True(t, expanded.review.view.IsExpanded(fakeTallFile))

	reloaded := reloadReview(t, expanded)

	assert.True(t, reloaded.review.view.IsExpanded(fakeTallFile), "the file is still drawn whole")
	assert.Contains(t, ansi.Strip(reviewFrameText(t, reloaded)), "func Four()")
}

// TestExpandRefusesAFileItCannotDrawMoreOf covers a file whose hunks already cover every line of
// it, where the key has nothing to add and has to say so rather than appearing not to have
// registered.
func TestExpandRefusesAFileItCannotDrawMoreOf(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	refused := pressExpand(t, m)

	assert.False(t, refused.review.view.IsExpanded(fakeFile))
	assert.Contains(t, refused.review.notice, "no more of that file")
}

// TestExpandRefusesAFoldedFile covers the file the reader has already read and folded away, which
// has no lines on screen for the key to add to.
func TestExpandRefusesAFoldedFile(t *testing.T) {
	m, _ := tallReview(t)

	folded, _ := m.toggleReviewViewed()
	m, ok := folded.(model)
	require.True(t, ok)
	require.True(t, m.review.view.IsViewed(fakeTallFile))

	refused := pressExpand(t, m)

	assert.False(t, refused.review.view.IsExpanded(fakeTallFile))
	assert.Contains(t, refused.review.notice, "folded away")
}

// TestExpandedFileDrawsNoHunkHeader covers the header of a run that is the whole file, which is
// counted rather than read because a header with nothing in it draws a blank row rather than
// no row.
func TestExpandedFileDrawsNoHunkHeader(t *testing.T) {
	m, _ := tallReview(t)

	require.Contains(t, ansi.Strip(reviewFrameText(t, m)), "@@", "the tight view heads its hunk")

	expanded := pressExpand(t, m)

	assert.NotContains(t, ansi.Strip(reviewFrameText(t, expanded)), "@@")

	// The file's name, then thirteen lines of the new side with the line they replaced among
	// them, and nothing else.
	assert.Equal(t, 15, expanded.review.view.Len(), "no row is drawn for the run's own header")
}

// rowOfNewLine is the row drawing one line of the new side, so a test can stand the cursor on a
// line it names rather than on whichever row an index happens to hit.
func rowOfNewLine(t *testing.T, m model, line int) int {
	t.Helper()

	for y := range m.review.view.Len() {
		if place, ok := m.review.view.GetPlace(y); ok && place.NewLine == line {
			return y
		}
	}

	require.FailNow(t, "no row draws that line")

	return 0
}

// reloadReview runs the load the catch-up key asks for and applies what it answered, which is the
// path a reader takes when the agent has written since they opened the diff.
func reloadReview(t *testing.T, m model) model {
	t.Helper()

	next, cmd := m.reloadReview()
	require.NotNil(t, cmd)

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)

	next.applyReviewLoaded(loaded)

	return next
}

// TestACommentCanBeWrittenOnAnExpandedLine covers the reason expanding is worth having at all: a
// reader unfolds a file because they want to say something about what they find there, and a line
// the patch did not draw carries both sides' numbers, so it anchors like any other.
func TestACommentCanBeWrittenOnAnExpandedLine(t *testing.T) {
	m, agents := tallReview(t)

	m = pressExpand(t, m)
	m.review.cursor = rowOfNewLine(t, m, 13)
	require.Contains(t, ansi.Strip(m.reviewView()), "func Four()")

	next, _ := m.updateReview(keyMsg("c"))
	m, ok := next.(model)
	require.True(t, ok)
	require.True(t, m.review.composing, "the line the expansion drew takes a comment")

	m = typeReviewMessage(t, m, "this one is unchanged and wrong")

	next, cmd := m.updateReview(tea.KeyMsg{Type: tea.KeyCtrlS})
	m, ok = next.(model)
	require.True(t, ok)
	require.NotNil(t, cmd)

	wrote, ok := cmd().(reviewWroteMsg)
	require.True(t, ok)
	require.NoError(t, wrote.err)

	require.NotNil(t, agents.created)
	assert.Equal(t, fakeTallFile, agents.created.GetFilePath())
	assert.Equal(t, int32(13), agents.created.GetNewStart(), "anchored to the line it was written on")
	assert.Equal(t, []string{" func Four() {}"}, agents.created.GetQuotedLines())
}
