package ui

import (
	"testing"

	"weave/internal/gittest"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pressExpand presses the expand key and hands back the screen it left.
func pressExpand(t *testing.T, m model) model {
	t.Helper()

	next, _ := m.routeScreenMessage(keyMsg("E"))
	expanded, ok := next.(model)
	require.True(t, ok)

	return expanded
}

// tallReview is a review screen over a file with lines the patch does not draw.
func tallReview(t *testing.T) (model, *fakeReviewAgents) {
	t.Helper()

	agents := &fakeReviewAgents{}
	m, _ := buildReviewModel(t, agents, gittest.NewTallRepo(t.TempDir()))

	return openReview(t, m), agents
}

// TestExpandDrawsTheLinesThePatchLeftOut covers the whole point of the key: the file's own text
// above and below the hunk.
func TestExpandDrawsTheLinesThePatchLeftOut(t *testing.T) {
	m, _ := tallReview(t)

	before := ansi.Strip(reviewFrameText(t, m))
	require.Contains(t, before, "func Two()", "the hunk is drawn to begin with")
	require.NotContains(t, before, "func Four()", "and the end of the file is not")

	after := ansi.Strip(reviewFrameText(t, pressExpand(t, m)))
	assert.Contains(t, after, "func Four()", "the lines below the hunk are drawn")
	assert.Contains(t, after, "import \"fmt\"", "and the lines above it")
}

// TestExpandGivesTheChangesBackAgain covers the key being a toggle.
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

	m.review.diff.cursor = rowOfNewLine(t, m, 7)
	before, ok := m.review.diff.view.GetPlace(m.review.diff.cursor)
	require.True(t, ok)

	expanded := pressExpand(t, m)

	after, ok := expanded.review.diff.view.GetPlace(expanded.review.diff.cursor)
	require.True(t, ok)
	assert.Equal(t, before, after, "the cursor is standing on the line it was standing on")
}

// TestExpansionSurvivesCatchingUp covers a reload.
func TestExpansionSurvivesCatchingUp(t *testing.T) {
	m, _ := tallReview(t)

	expanded := pressExpand(t, m)
	require.True(t, expanded.review.diff.view.IsExpanded(gittest.TallFile))

	reloaded := reloadReview(t, expanded)

	assert.True(t, reloaded.review.diff.view.IsExpanded(gittest.TallFile), "the file is still drawn whole")
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

	assert.False(t, refused.review.diff.view.IsExpanded(gittest.File))
	assert.Contains(t, refused.review.notice, "no more of that file")
}

// TestExpandRefusesAFoldedFile covers the file the reader has already read and folded away, which
// has no lines on screen for the key to add to.
func TestExpandRefusesAFoldedFile(t *testing.T) {
	m, _ := tallReview(t)

	m.review.toggleViewed(&m, m.shared())
	require.True(t, m.review.diff.view.IsViewed(gittest.TallFile))

	refused := pressExpand(t, m)

	assert.False(t, refused.review.diff.view.IsExpanded(gittest.TallFile))
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
	assert.Equal(t, 15, expanded.review.diff.view.Len(), "no row is drawn for the run's own header")
}

// rowOfNewLine is the row drawing one line of the new side, so a test can stand the cursor on a
// line it names rather than on whichever row an index happens to hit.
func rowOfNewLine(t *testing.T, m model, line int) int {
	t.Helper()

	for y := range m.review.diff.view.Len() {
		if place, ok := m.review.diff.view.GetPlace(y); ok && place.NewLine == line {
			return y
		}
	}

	require.FailNow(t, "no row draws that line")

	return 0
}

// reloadReview runs the load the catch-up key asks for and applies what it answered.
func reloadReview(t *testing.T, m model) model {
	t.Helper()

	reloaded, _ := reloadReviewReading(t, m)

	return reloaded
}

// reloadReviewReading is the same reload, handing back what the load answered as well as the
// screen it left.
func reloadReviewReading(t *testing.T, m model) (model, reviewLoadedMsg) {
	t.Helper()

	holdServedThreads(&m)
	cmd := m.review.reload(m.shared())
	require.NotNil(t, cmd)

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)

	m.review.applyLoaded(&m, m.shared(), loaded)

	return m, loaded
}

// TestACommentCanBeWrittenOnAnExpandedLine covers the reason expanding is worth having at all: a
// reader unfolds a file because they want to say something about what they find there, and a line
// the patch did not draw carries both sides' numbers, so it anchors like any other.
func TestACommentCanBeWrittenOnAnExpandedLine(t *testing.T) {
	m, agents := tallReview(t)

	m = pressExpand(t, m)
	m.review.diff.cursor = rowOfNewLine(t, m, 13)
	require.Contains(t, ansi.Strip(m.review.view(m.shared())), "func Four()")

	next, _ := m.routeScreenMessage(keyMsg("c"))
	m, ok := next.(model)
	require.True(t, ok)
	require.True(t, m.review.threads.composing, "the line the expansion drew takes a comment")

	m = typeReviewMessage(t, m, "this one is unchanged and wrong")

	next, cmd := m.routeScreenMessage(keyMsg("ctrl+s"))
	_, ok = next.(model)
	require.True(t, ok)
	require.NotNil(t, cmd)

	wrote, ok := cmd().(reviewWroteMsg)
	require.True(t, ok)
	require.NoError(t, wrote.err)

	require.NotNil(t, agents.created)
	assert.Equal(t, gittest.TallFile, agents.created.GetFilePath())
	assert.Equal(t, int32(13), agents.created.GetNewStart(), "anchored to the line it was written on")
	assert.Equal(t, []string{" func Four() {}"}, agents.created.GetQuotedLines())
}

// TestCollapsingPutsItsCommentOffScreenRatherThanCallingItChanged covers a comment written on a
// line only expanding drew, whose line is still there after the file is drawn tight again, so the
// heading for code the agent has rewritten would say something false about it.
func TestCollapsingPutsItsCommentOffScreenRatherThanCallingItChanged(t *testing.T) {
	m, agents := tallReview(t)

	m = pressExpand(t, m)
	m.review.diff.cursor = rowOfNewLine(t, m, 13)

	anchor, ok := m.review.diff.view.GetAnchor(m.review.diff.cursor)
	require.True(t, ok)
	require.Equal(t, 13, anchor.NewStart, "the comment is on a line only expanding draws")

	agents.threads = []*agentv1.ReviewThread{{
		Id:          "t1",
		FilePath:    anchor.FilePath,
		OldStart:    int32(anchor.OldStart),
		NewStart:    int32(anchor.NewStart),
		QuotedLines: anchor.QuotedLines,
		Messages: []*agentv1.ReviewMessage{{
			Id:         "m1",
			AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
			Body:       "unchanged and wrong",
		}},
	}}

	m = reloadReview(t, m)
	require.True(t, m.review.diff.view.IsExpanded(gittest.TallFile), "the file is still open")

	collapsed := ansi.Strip(reviewFrameText(t, pressExpand(t, m)))

	assert.Contains(t, collapsed, "outside what is on screen",
		"the comment is off screen by the reader's own choice")
	assert.NotContains(t, collapsed, "has changed",
		"nothing about the agent's work moved")
}

// TestFoldingAnExpandedFileStaysFolded covers the mark taken while the file is drawn whole, which
// has to describe the change to that file rather than the way the reader chose to draw it, or the
// file comes back unread on the next catch-up and can never be cleared from the list.
func TestFoldingAnExpandedFileStaysFolded(t *testing.T) {
	m, agents := tallReview(t)

	m = pressExpand(t, m)
	require.True(t, m.review.diff.view.IsExpanded(gittest.TallFile))

	next, cmd := m.routeScreenMessage(keyMsg("v"))
	m, ok := next.(model)
	require.True(t, ok)
	require.NotNil(t, cmd)

	wrote, ok := cmd().(reviewWroteMsg)
	require.True(t, ok)
	require.NoError(t, wrote.err)
	require.True(t, m.review.diff.view.IsViewed(gittest.TallFile), "the file folds away on screen")

	require.NotNil(t, agents.marked)

	reloaded, loaded := reloadReviewReading(t, m)

	assert.Equal(t, patchFingerprint(t, loaded, gittest.TallFile), agents.marked.GetFingerprint(),
		"the mark carries the fingerprint of the file as the patch holds it")
	assert.True(t, reloaded.review.diff.view.IsViewed(gittest.TallFile), "and it is still read after a reload")
}

// patchFingerprint is the fingerprint a load computed for a file.
func patchFingerprint(t *testing.T, loaded reviewLoadedMsg, path string) string {
	t.Helper()

	for i := range loaded.patch.Files {
		if loaded.patch.Files[i].Path() == path {
			return loaded.patch.Files[i].Fingerprint()
		}
	}

	require.FailNow(t, "the patch draws no such file")

	return ""
}

// TestExpansionSurvivesTheDiffBeingPutDown covers the reload that draws nothing until the whole
// change arrives, which is the one reload with no view to take the opened files from.
func TestExpansionSurvivesTheDiffBeingPutDown(t *testing.T) {
	m, _ := tallReview(t)

	expanded := pressExpand(t, m)
	require.True(t, expanded.review.diff.view.IsExpanded(gittest.TallFile))

	// An index past the end of the recut list is what puts the diff down.
	expanded.review.diff.chapter = len(expanded.review.diff.chapters)

	reload := expanded.review.reload(expanded.shared())
	require.NotNil(t, reload)

	loaded, ok := reload().(reviewLoadedMsg)
	require.True(t, ok)

	again := expanded.review.applyLoaded(&expanded, expanded.shared(), loaded)
	require.Nil(t, expanded.review.diff.view, "nothing is drawn until the whole change lands")
	require.NotNil(t, again)

	answer, ok := again().(reviewLoadedMsg)
	require.True(t, ok)
	expanded.review.applyLoaded(&expanded, expanded.shared(), answer)

	require.NotNil(t, expanded.review.diff.view)
	assert.True(t, expanded.review.diff.view.IsExpanded(gittest.TallFile), "the file is still drawn whole")
	assert.Contains(t, ansi.Strip(reviewFrameText(t, expanded)), "func Four()")
}
