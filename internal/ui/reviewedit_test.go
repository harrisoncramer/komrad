package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"weave/internal/git"
	"weave/internal/ui/layout"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func editorKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")} }

func composeKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")} }

// lastRespawnOf is the command a pane was last handed and where it was rooted, which is how the
// editor's own pane is read apart from the stage being drawn again beside it.
func lastRespawnOf(t *testing.T, tmux *recordingTmux, pane string) (argv, dir string) {
	t.Helper()

	for i := len(tmux.respawnPanes) - 1; i >= 0; i-- {
		if tmux.respawnPanes[i] == pane {
			return tmux.respawns[i], tmux.respawnDirs[i]
		}
	}

	t.Fatalf("nothing was run in %s", pane)

	return "", ""
}

// pressEdit walks the whole path the editor key takes: the round is checked out off the UI
// thread, and the message that lands is what opens the editor on it.
func pressEdit(t *testing.T, m model) model {
	t.Helper()

	next, cmd := m.updateReview(editorKey())
	m = next.(model)
	if cmd == nil {
		return m
	}

	msg := cmd()
	ready, ok := msg.(roundReadyMsg)
	if !ok {
		return m
	}

	require.NoError(t, ready.err)

	opened, _ := m.applyRoundReady(ready)

	return opened.(model)
}

// closeEdit shuts the editor and walks the read of the checkout that follows it, which is what
// turns whatever was saved into the suggestions the panel holds.
func closeEdit(t *testing.T, m model, shut func(model) (tea.Model, tea.Cmd)) model {
	t.Helper()

	next, cmd := shut(m)
	m = next.(model)
	require.NotNil(t, cmd, "closing the editor reads the checkout back")

	edited, ok := cmd().(roundEditedMsg)
	require.True(t, ok)
	require.NoError(t, edited.err)

	m.applyRoundEdited(edited)

	return m
}

// closeEditKey is the ordinary close, by the key that opened it.
func closeEditKey(t *testing.T, m model) model {
	t.Helper()

	return closeEdit(t, m, func(m model) (tea.Model, tea.Cmd) { return m.closeReviewEdit() })
}

// TestReviewEditorOpensTheRoundsCopy covers the editor key: a split under the diff, the round's
// own copy of the file the cursor is in opened on that line, and the keyboard handed to it.
func TestReviewEditorOpensTheRoundsCopy(t *testing.T) {
	dir := reviewRepo(t)
	m, tmux := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = pressEdit(t, m)

	require.Equal(t, infoEditFile, m.info.kind)
	assert.Equal(t, []string{"%1"}, tmux.belowAnchors, "the split hangs off the stage")

	opened, rooted := lastRespawnOf(t, tmux, "%2")
	assert.Equal(t, m.review.roundDir, rooted, "the editor is rooted in the round, not the worktree")
	assert.NotEqual(t, m.review.root, rooted, "which is a different tree from the one the agent writes to")
	assert.Contains(t, opened, "vi +1 'greet.go'")
	assert.Equal(t, []string{"%2"}, tmux.selected, "the editor is what the user types into")

	// The editor is a pane of its own, so the window goes back to being shared while it is up.
	assert.False(t, tmux.zoomed)
}

// TestReviewEditorChecksTheRoundOut pins what the editor is opened on: a detached checkout of the
// round's snapshot, holding the code as the round froze it rather than as the worktree has it now.
func TestReviewEditorChecksTheRoundOut(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = pressEdit(t, m)
	require.NotEmpty(t, m.review.roundDir)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "greet.go"),
		[]byte("package greet\n\nfunc Hello() string {\n\treturn \"the agent moved on\"\n}\n"), 0o644))

	held, err := os.ReadFile(filepath.Join(m.review.roundDir, "greet.go"))
	require.NoError(t, err)

	assert.Contains(t, string(held), "goodbye", "the round holds what it froze")
	assert.NotContains(t, string(held), "the agent moved on", "and not what the agent wrote after it")
}

// TestReviewEditorLeavesTheWorktreeAlone is the whole point of editing a round: a save from the
// reviewer reaches nothing the agent is writing to.
func TestReviewEditorLeavesTheWorktreeAlone(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = pressEdit(t, m)
	writeRoundFile(t, m, "package greet\n\nfunc Hello() string {\n\treturn \"the reviewer's idea\"\n}\n")

	m = closeEditKey(t, m)

	worktree, err := os.ReadFile(filepath.Join(dir, "greet.go"))
	require.NoError(t, err)

	assert.NotContains(t, string(worktree), "the reviewer's idea", "the agent's file is untouched")
	require.Len(t, m.review.suggestions, 1, "and the edit is held as a suggestion instead")
}

// TestReviewEditorRunsTheConfiguredEditor covers an EDITOR of several words, which reaches the
// pane as the words it is made of rather than as the name of a program with spaces in it.
func TestReviewEditorRunsTheConfiguredEditor(t *testing.T) {
	dir := reviewRepo(t)
	m, tmux := reviewModel(t, &fakeReviewAgents{}, dir)
	m.editor = "nvim --clean"
	m = openReview(t, m)

	m = pressEdit(t, m)

	opened, _ := lastRespawnOf(t, tmux, "%2")
	assert.Contains(t, opened, "nvim --clean +1 'greet.go'")
}

// TestReviewEditorTogglesShut covers the second press: the same line is what opened the editor,
// so it is what takes it away.
func TestReviewEditorTogglesShut(t *testing.T) {
	dir := reviewRepo(t)
	m, tmux := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = pressEdit(t, m)
	m = pressEdit(t, m)

	assert.Equal(t, infoNothing, m.info.kind)
	assert.Contains(t, tmux.killed, "%2")
	assert.Equal(t, "close the editor", model{info: infoState{kind: infoEditFile}}.reviewEditorDesc())
	assert.Equal(t, "open in your editor", m.reviewEditorDesc())
}

// TestReviewEditorFollowsTheCursor covers moving between presses: the editor is opened again on
// wherever the cursor has got to rather than toggled shut.
func TestReviewEditorFollowsTheCursor(t *testing.T) {
	dir := reviewRepo(t)
	m, tmux := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = pressEdit(t, m)
	next, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	m = pressEdit(t, next.(model))

	require.Equal(t, infoEditFile, m.info.kind)
	reopened, _ := lastRespawnOf(t, tmux, "%2")
	assert.Contains(t, reopened, "vi +5 'greet.go'")
}

// TestReviewEditCloseHoldsTheEdit pins every way the editor closes reading what was written in the
// round, since that is the only moment weave can tell an edit from an untouched file.
func TestReviewEditCloseHoldsTheEdit(t *testing.T) {
	tests := []struct {
		name  string
		close func(model) (tea.Model, tea.Cmd)
	}{
		{
			name:  "the toggle key closes it",
			close: func(m model) (tea.Model, tea.Cmd) { return m.closeReviewEdit() },
		},
		{
			name: "the editor quits and the keys come back to the panel",
			close: func(m model) (tea.Model, tea.Cmd) {
				m.frame.Close(layout.RegionInfo)

				return m.route(tea.FocusMsg{})
			},
		},
		{
			name: "the tick notices the pane gone, where no focus event came",
			close: func(m model) (tea.Model, tea.Cmd) {
				m.frame.Close(layout.RegionInfo)

				return m.reconcileEditor()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := reviewRepo(t)
			m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
			m = openReview(t, m)

			m = pressEdit(t, m)
			require.Equal(t, infoEditFile, m.info.kind, "the editor is up")

			writeRoundFile(t, m, "package greet\n\nfunc Hello() string {\n\treturn \"edited in the editor\"\n}\n")

			m = closeEdit(t, m, tt.close)

			require.Len(t, m.review.suggestions, 1, "the edit is held as a suggestion")
			held := m.review.suggestions[0]
			assert.Equal(t, "greet.go", held.anchor.FilePath)
			assert.Equal(t, 4, held.anchor.NewStart)
			assert.Equal(t, 4, held.anchor.NewEnd)
			assert.Equal(t, []string{"\treturn \"edited in the editor\""}, held.lines)
		})
	}
}

// TestReviewEditRevertedTakesTheSuggestionBack covers the reviewer changing their mind in the editor:
// a file put back the way the round holds it proposes nothing.
func TestReviewEditRevertedTakesTheSuggestionBack(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = pressEdit(t, m)
	writeRoundFile(t, m, "package greet\n\nfunc Hello() string {\n\treturn \"an idea\"\n}\n")

	m = closeEditKey(t, m)
	require.Len(t, m.review.suggestions, 1)

	m = pressEdit(t, m)
	writeRoundFile(t, m, "package greet\n\nfunc Hello() string {\n\treturn \"goodbye\"\n}\n")

	m = closeEditKey(t, m)

	assert.Empty(t, m.review.suggestions, "putting the file back takes the suggestion with it")
}

// TestReviewSuggestionGoesWithTheComment covers the whole point of holding an edit: the comment
// written next carries it, pinned where the edit was made rather than where the cursor is.
func TestReviewSuggestionGoesWithTheComment(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m = pressEdit(t, m)
	writeRoundFile(t, m, "package greet\n\nfunc Hello() string {\n\treturn \"farewell\"\n}\n")

	m = closeEditKey(t, m)

	shown := ansi.Strip(m.reviewView())
	assert.Contains(t, shown, "edit held for greet.go:4", "the sidebar says where it will land")

	next, _ := m.updateReview(composeKey())
	m = next.(model)
	require.True(t, m.review.composing)

	m.review.editor.SetValue("say farewell instead")

	next, cmd := m.updateReview(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(model)
	require.NotNil(t, cmd)
	cmd()

	require.NotNil(t, agents.created)
	assert.Equal(t, "greet.go", agents.created.GetFilePath())
	assert.Equal(t, int32(4), agents.created.GetNewStart())
	assert.Equal(t, []string{"\treturn \"farewell\""}, agents.created.GetSuggestion())
	assert.Equal(t, "say farewell instead", agents.created.GetBody())
	assert.Empty(t, m.review.suggestions, "and the panel is not still holding it")

	// The reviewer's text is still in the checkout, so reading it again must not offer back what
	// they have already answered for.
	m = pressEdit(t, m)
	m = closeEditKey(t, m)
	assert.Empty(t, m.review.suggestions, "a proposal already sent is not offered again")
}

// TestReviewSuggestionTurnedDownOnCancel covers the composer being the place weave asks whether to
// send an edit, so leaving it without writing anything is the answer.
func TestReviewSuggestionTurnedDownOnCancel(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m = pressEdit(t, m)
	writeRoundFile(t, m, "package greet\n\nfunc Hello() string {\n\treturn \"farewell\"\n}\n")

	m = closeEditKey(t, m)

	next, _ := m.updateReview(composeKey())
	next, _ = next.(model).updateReview(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)

	assert.Empty(t, m.review.suggestions)
	assert.Nil(t, agents.created, "nothing was sent")
	assert.Contains(t, m.review.notice, "turned down")

	m = pressEdit(t, m)
	m = closeEditKey(t, m)
	assert.Empty(t, m.review.suggestions, "a proposal turned down is not offered again")
}

// wideRepo is a file long enough that its diff draws two hunks with undrawn lines between them,
// which is where an edit in one hunk, an edit in the other, and an edit in the gap all differ.
func wideRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	lines := make([]string, 40)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}

	gitIn(t, dir, "init", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "wide.txt"),
		[]byte(strings.Join(lines, "\n")+"\n"), 0o644))
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "first")

	lines[2] = "line 3 as the agent wrote it"
	lines[34] = "line 35 as the agent wrote it"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "wide.txt"),
		[]byte(strings.Join(lines, "\n")+"\n"), 0o644))

	return dir
}

// writeWide saves the round's copy of the long file with the named lines replaced, which is the
// reviewer typing in two places at once.
func writeWide(t *testing.T, m model, changes map[int]string) {
	t.Helper()

	require.NotEmpty(t, m.review.roundDir)

	held, err := os.ReadFile(filepath.Join(m.review.roundDir, "wide.txt"))
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSuffix(string(held), "\n"), "\n")
	for at, text := range changes {
		lines[at-1] = text
	}

	require.NoError(t, os.WriteFile(filepath.Join(m.review.roundDir, "wide.txt"),
		[]byte(strings.Join(lines, "\n")+"\n"), 0o644))
}

// TestReviewTwoEditsInOneFileAreTwoSuggestions is the case coalescing used to lose: two changes in
// one file, far enough apart that the lines between them are not drawn, so one span over both
// could be pinned nowhere.
func TestReviewTwoEditsInOneFileAreTwoSuggestions(t *testing.T) {
	dir := wideRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m = pressEdit(t, m)
	writeWide(t, m, map[int]string{3: "line 3 the reviewer's way", 35: "line 35 the reviewer's way"})
	m = closeEditKey(t, m)

	require.Len(t, m.review.suggestions, 2, "each run is proposed on its own")
	assert.Equal(t, 3, m.review.suggestions[0].anchor.NewStart)
	assert.Equal(t, []string{"line 3 the reviewer's way"}, m.review.suggestions[0].lines)
	assert.Equal(t, 35, m.review.suggestions[1].anchor.NewStart)
	assert.Equal(t, []string{"line 35 the reviewer's way"}, m.review.suggestions[1].lines)

	shown := ansi.Strip(m.reviewView())
	assert.Contains(t, shown, "edit held for wide.txt:3")
	assert.Contains(t, shown, "1 more behind it")

	// The first comment carries the first, and the second is still standing behind it.
	next, _ := m.updateReview(composeKey())
	m = next.(model)
	m.review.editor.SetValue("first")

	next, cmd := m.updateReview(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(model)
	require.NotNil(t, cmd)
	cmd()

	require.NotNil(t, agents.created)
	assert.Equal(t, int32(3), agents.created.GetNewStart())
	assert.Equal(t, []string{"line 3 the reviewer's way"}, agents.created.GetSuggestion())

	require.Len(t, m.review.suggestions, 1)
	assert.Equal(t, 35, m.review.suggestions[0].anchor.NewStart)
}

// TestReviewEditOutsideTheDiffIsNamedAndKept covers the reviewer scrolling out of the hunk in the
// editor: the diff has no line to pin that edit to, so it is named rather than silently lost, and
// the edits that can be pinned are unaffected.
func TestReviewEditOutsideTheDiffIsNamedAndKept(t *testing.T) {
	dir := wideRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = pressEdit(t, m)
	writeWide(t, m, map[int]string{3: "line 3 the reviewer's way", 20: "line 20 the reviewer's way"})
	m = closeEditKey(t, m)

	require.Len(t, m.review.suggestions, 1, "the edit inside a hunk still stands")
	assert.Equal(t, 3, m.review.suggestions[0].anchor.NewStart)

	require.Len(t, m.review.adrift, 1)
	assert.Equal(t, 20, m.review.adrift[0].NewStart)

	shown := ansi.Strip(m.reviewView())
	assert.Contains(t, shown, "wide.txt:20 is not in the diff")
	assert.Contains(t, shown, "stays in the round's checkout")

	held, err := os.ReadFile(filepath.Join(m.review.roundDir, "wide.txt"))
	require.NoError(t, err)
	assert.Contains(t, string(held), "line 20 the reviewer's way", "and the text really is still there")
}

// TestReviewReloadKeepsTheCheckoutToReap covers the stale bar being answered: a new round drops the
// edits held against the last one, and says so, but the checkout is still there to be taken away.
func TestReviewReloadKeepsTheCheckoutToReap(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = pressEdit(t, m)
	at := m.review.roundDir
	writeRoundFile(t, m, "package greet\n\nfunc Hello() string {\n\treturn \"an idea\"\n}\n")
	m = closeEditKey(t, m)
	require.Len(t, m.review.suggestions, 1)

	// The agent writes, so reloading opens a round the held edit no longer names lines of.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "greet.go"),
		[]byte("package greet\n\nfunc Hello() string {\n\treturn \"the agent moved on\"\n}\n"), 0o644))

	reloaded, cmd := m.reloadReview()
	require.NotNil(t, cmd)

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)
	require.NoError(t, loaded.err)

	m = reloaded
	m.applyReviewLoaded(loaded)

	assert.Empty(t, m.review.suggestions, "the edits went with the round they were made against")
	assert.Contains(t, m.review.notice, "the round moved on")
	require.Equal(t, at, m.review.roundDir, "and the checkout is still ours to reap")

	// What is in that checkout is still the reviewer's, so the press that would take it says so
	// first, whether or not the reload already said the round had moved.
	m = pressEdit(t, m)
	require.Equal(t, at, m.review.replacing)
	assert.Contains(t, m.review.notice, "press again")

	// The next edit has to be of the round on screen, so the checkout is moved onto it rather
	// than left standing on the one the reader has already left.
	m = pressEdit(t, m)
	assert.Equal(t, m.review.round.GetSnapshotSha(), m.review.roundSha)
	assert.Equal(t, m.review.round.GetSnapshotSha(), headOf(t, at))

	held, err := os.ReadFile(filepath.Join(at, "greet.go"))
	require.NoError(t, err)
	assert.Contains(t, string(held), "the agent moved on", "which is the text the diff is drawing")

	_, cmd = m.leaveReview()
	require.NotNil(t, cmd)
	drainCmd(cmd)

	assert.NoDirExists(t, at)
}

// TestReviewReloadWarnsWithTheEditorStillUp covers catching up without closing the editor: nothing
// has been read back, so the queue is empty, but the checkout still holds whatever was saved into
// it and the next edit checks the new round out over the top of it.
func TestReviewReloadWarnsWithTheEditorStillUp(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = pressEdit(t, m)
	at := m.review.roundDir
	writeRoundFile(t, m, "package greet\n\nfunc Hello() string {\n\treturn \"an idea\"\n}\n")
	require.Equal(t, infoEditFile, m.info.kind, "the editor is still up")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "greet.go"),
		[]byte("package greet\n\nfunc Hello() string {\n\treturn \"the agent moved on\"\n}\n"), 0o644))

	reloaded, cmd := m.reloadReview()
	require.NotNil(t, cmd)

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)
	require.NoError(t, loaded.err)

	m = reloaded
	m.applyReviewLoaded(loaded)

	require.Empty(t, m.review.suggestions, "nothing was ever read back")
	assert.Contains(t, m.review.notice, "the round moved on",
		"and the reader is told before the text goes")

	held, err := os.ReadFile(filepath.Join(at, "greet.go"))
	require.NoError(t, err)
	assert.Contains(t, string(held), "an idea", "which is while it is still there to rescue")
}

// TestReviewEditsAgainstAStaleRoundAreRefused covers the round moving while the editor is up: the
// runs are numbered against the checkout's snapshot, so holding them against a diff drawn from a
// different one would pin the reviewer's words to lines they never looked at.
func TestReviewEditsAgainstAStaleRoundAreRefused(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = pressEdit(t, m)
	writeRoundFile(t, m, "package greet\n\nfunc Hello() string {\n\treturn \"an idea\"\n}\n")

	// The read of the checkout is taken before the round moves, which is the order a slow editor
	// and a busy agent put these in.
	_, cmd := m.closeReviewEdit()
	require.NotNil(t, cmd)

	edited, ok := cmd().(roundEditedMsg)
	require.True(t, ok)
	require.NoError(t, edited.err)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "greet.go"),
		[]byte("package greet\n\nfunc Hello() string {\n\treturn \"the agent moved on\"\n}\n"), 0o644))

	reloaded, cmd := m.reloadReview()
	require.NotNil(t, cmd)

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)
	require.NoError(t, loaded.err)

	m = reloaded
	m.applyReviewLoaded(loaded)
	m.applyRoundEdited(edited)

	assert.Empty(t, m.review.suggestions, "nothing is pinned against a round nobody read")
	assert.Empty(t, m.review.adrift)
	assert.Contains(t, m.review.notice, "moved on while you were editing")
}

// headOf is the commit a checkout is standing on, which is what says whether it is the round the
// diff on screen was drawn from.
func headOf(t *testing.T, at string) string {
	t.Helper()

	head, err := git.Run(t.Context(), at, "rev-parse", "HEAD")
	require.NoError(t, err)

	return head
}

// TestReviewEditRefusesASecondCheckout covers the key being pressed twice before the first
// checkout lands, which used to start a second one against the same directory.
func TestReviewEditRefusesASecondCheckout(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	next, first := m.updateReview(editorKey())
	m = next.(model)
	require.NotNil(t, first)

	next, second := m.updateReview(editorKey())
	m = next.(model)
	assert.Nil(t, second, "the second press waits for the first checkout")

	ready, ok := first().(roundReadyMsg)
	require.True(t, ok)
	require.NoError(t, ready.err)

	opened, _ := m.applyRoundReady(ready)
	m = opened.(model)

	require.Equal(t, infoEditFile, m.info.kind)
	assert.False(t, m.review.materialising, "and the next press is free to go")
}

// TestReviewEditReapsTheCheckout covers leaving the screen: the checkout was cut for one reader
// and there is nobody left to be reading it.
func TestReviewEditReapsTheCheckout(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = pressEdit(t, m)
	at := m.review.roundDir
	require.DirExists(t, at)

	_, cmd := m.leaveReview()
	require.NotNil(t, cmd)
	drainCmd(cmd)

	assert.NoDirExists(t, at, "the checkout went with the screen")
}

// TestReviewEditCloseCaughtByTheTick pins the tick catching the editor gone as well, since a
// tmux that reports no focus event would otherwise leave the diff behind it stale.
func TestReviewEditCloseCaughtByTheTick(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = pressEdit(t, m)
	require.Equal(t, infoEditFile, m.info.kind, "the editor is up")

	m.frame.Close(layout.RegionInfo)
	ticked, _ := m.route(refreshTickMsg{at: time.Now()})

	assert.Equal(t, infoNothing, ticked.(model).info.kind, "the tick noticed the editor gone")
}

// writeRoundFile is the reviewer saving in the editor, which writes to the round's own checkout
// and to nothing the agent can see.
func writeRoundFile(t *testing.T, m model, content string) {
	t.Helper()

	require.NotEmpty(t, m.review.roundDir, "the round has been checked out")
	require.NoError(t, os.WriteFile(filepath.Join(m.review.roundDir, "greet.go"), []byte(content), 0o644))
}

// drainCmd runs a command and every command a batch of them holds, since a batch is what the
// panel returns when one keypress does two things.
func drainCmd(cmd tea.Cmd) {
	if cmd == nil {
		return
	}

	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, next := range batch {
			drainCmd(next)
		}
	}
}

// TestReviewKeepsDrawingAfterAReload pins the diff still being drawn after a reload, since a
// reload that lost the patch would leave the sidebar moving over an empty half.
func TestReviewKeepsDrawingAfterAReload(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	reloaded, cmd := m.reloadReview()
	require.NotNil(t, cmd)

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)
	require.NoError(t, loaded.err)

	m = reloaded
	m.applyReviewLoaded(loaded)

	_, ok = m.reviewSplit()
	require.True(t, ok, "the window is still divided between the diff and the sidebar")

	moved, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})

	assert.NotEqual(t, m.review.cursor, moved.(model).review.cursor, "and the cursor actually moved")
	assert.Contains(t, reviewFrameText(t, moved.(model)), "greet.go", "and the diff is still drawn")
}

// TestReviewEditReadsUnchangedFileQuietly covers closing the editor without having saved
// anything, which proposes nothing and says nothing.
func TestReviewEditReadsUnchangedFileQuietly(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = pressEdit(t, m)
	m = closeEditKey(t, m)

	assert.Empty(t, m.review.suggestions)
	assert.Empty(t, strings.TrimSpace(m.review.notice))
}

// TestReviewLeavingKeepsAnUncarriedEdit is the bug this file was opened for: leaving the screen
// force-removed the checkout, so an edit no comment had carried yet went with it in silence.
func TestReviewLeavingKeepsAnUncarriedEdit(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = pressEdit(t, m)
	at := m.review.roundDir
	writeRoundFile(t, m, "package greet\n\nfunc Hello() string {\n\treturn \"the reviewer's idea\"\n}\n")
	m = closeEditKey(t, m)
	require.Len(t, m.review.suggestions, 1, "the edit is held, waiting for a comment")

	_, cmd := m.leaveReview()
	require.NotNil(t, cmd)
	drainCmd(cmd)

	require.DirExists(t, at, "the checkout is kept, since it is the only place that text lives")

	held, err := os.ReadFile(filepath.Join(at, "greet.go"))
	require.NoError(t, err)
	assert.Contains(t, string(held), "the reviewer's idea")
}

// TestReviewPicksAnUncarriedEditBackUp covers the other half: the screen opening again on the same
// round finds what was left in the checkout and offers it, without the editor being opened first.
func TestReviewPicksAnUncarriedEditBackUp(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = pressEdit(t, m)
	at := m.review.roundDir
	writeRoundFile(t, m, "package greet\n\nfunc Hello() string {\n\treturn \"the reviewer's idea\"\n}\n")
	m = closeEditKey(t, m)

	left, cmd := m.leaveReview()
	drainCmd(cmd)

	m = openReview(t, left.(model))

	require.Len(t, m.review.suggestions, 1, "the edit is offered again")
	assert.Equal(t, "greet.go", m.review.suggestions[0].anchor.FilePath)
	assert.Equal(t, 4, m.review.suggestions[0].anchor.NewStart)
	assert.Equal(t, []string{"\treturn \"the reviewer's idea\""}, m.review.suggestions[0].lines)
	assert.Equal(t, at, m.review.roundDir, "and the checkout is ours again")
	assert.Equal(t, m.review.round.GetSnapshotSha(), m.review.roundSha)

	assert.Contains(t, ansi.Strip(m.reviewView()), "edit held for greet.go:4")
}

// TestReviewLeftoverOfAnEarlierRoundIsNamed covers coming back to an agent that has written since:
// the leftover names lines of a round nobody is looking at, so it cannot be offered, and the next
// edit checks the round on screen out over the top of it.
func TestReviewLeftoverOfAnEarlierRoundIsNamed(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = pressEdit(t, m)
	writeRoundFile(t, m, "package greet\n\nfunc Hello() string {\n\treturn \"the reviewer's idea\"\n}\n")
	m = closeEditKey(t, m)

	left, cmd := m.leaveReview()
	drainCmd(cmd)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "greet.go"),
		[]byte("package greet\n\nfunc Hello() string {\n\treturn \"the agent moved on\"\n}\n"), 0o644))

	m = openReview(t, left.(model))

	assert.Empty(t, m.review.suggestions, "nothing is pinned against a round nobody is reading")
	assert.Empty(t, m.review.roundDir, "and the checkout is not adopted as this round's")
	assert.Contains(t, m.review.notice, "an edit of an earlier round is still in its checkout")
}

// TestReviewLeavingReapsACarriedEdit covers the checkout still going when there is nothing in it to
// keep, since a comment that carried the edit put it somewhere durable.
func TestReviewLeavingReapsACarriedEdit(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m = pressEdit(t, m)
	at := m.review.roundDir
	writeRoundFile(t, m, "package greet\n\nfunc Hello() string {\n\treturn \"farewell\"\n}\n")
	m = closeEditKey(t, m)

	next, _ := m.updateReview(composeKey())
	m = next.(model)
	m.review.editor.SetValue("say farewell instead")

	next, cmd := m.updateReview(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(model)
	require.NotNil(t, cmd)
	cmd()

	require.NotNil(t, agents.created, "the comment carried it to the agent")
	require.Empty(t, m.review.suggestions, "and the panel is not still holding it")

	held, err := os.ReadFile(filepath.Join(at, "greet.go"))
	require.NoError(t, err)
	assert.Contains(t, string(held), "goodbye", "the run went back the way the round holds it")
	assert.NotContains(t, string(held), "farewell", "so the checkout is not still claiming it")

	left, cmd := m.leaveReview()
	require.NotNil(t, cmd)
	drainCmd(cmd)

	assert.NoDirExists(t, at, "a checkout holding nothing goes with the screen")

	// Which is the whole point: coming back must not offer a proposal that is already on a thread,
	// since the composer prefers a held edit over the thread under the cursor and would send it
	// a second time.
	m = openReview(t, left.(model))
	assert.Empty(t, m.review.suggestions, "a proposal already carried is not offered again")
}

// TestReviewCarriedEditLeavesTheOtherRunsStanding covers two edits in one file with only one of
// them sent: the run that went back is the one the comment took, and the other is still there to
// be carried by a comment of its own.
func TestReviewCarriedEditLeavesTheOtherRunsStanding(t *testing.T) {
	dir := wideRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m = pressEdit(t, m)
	at := m.review.roundDir
	writeWide(t, m, map[int]string{3: "line 3 the reviewer's way", 35: "line 35 the reviewer's way"})
	m = closeEditKey(t, m)
	require.Len(t, m.review.suggestions, 2)

	next, _ := m.updateReview(composeKey())
	m = next.(model)
	m.review.editor.SetValue("the first one")

	next, cmd := m.updateReview(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(model)
	require.NotNil(t, cmd)
	cmd()

	require.NotNil(t, agents.created)
	require.Equal(t, int32(3), agents.created.GetNewStart(), "the first run went")

	held, err := os.ReadFile(filepath.Join(at, "wide.txt"))
	require.NoError(t, err)
	assert.Contains(t, string(held), "line 3 as the agent wrote it",
		"the carried run is back the way the round holds it")
	assert.NotContains(t, string(held), "line 3 the reviewer's way")
	assert.Contains(t, string(held), "line 35 the reviewer's way", "and the one nobody carried stands")

	left, cmd := m.leaveReview()
	drainCmd(cmd)
	require.DirExists(t, at, "so the checkout is kept for it")

	m = openReview(t, left.(model))
	require.Len(t, m.review.suggestions, 1, "and only the uncarried run is offered again")
	assert.Equal(t, 35, m.review.suggestions[0].anchor.NewStart)
}

// TestReviewLeavingKeepsAFileWrittenFromNothing covers a file the reviewer created in the checkout,
// which no diff of the round reports and which a force removal deletes with everything else.
func TestReviewLeavingKeepsAFileWrittenFromNothing(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = pressEdit(t, m)
	at := m.review.roundDir
	require.NoError(t, os.WriteFile(filepath.Join(at, "reviewer-scratch.txt"), []byte("what I want instead\n"), 0o644))
	m = closeEditKey(t, m)
	require.Empty(t, m.review.suggestions, "a file from nothing proposes nothing")

	_, cmd := m.leaveReview()
	require.NotNil(t, cmd)
	drainCmd(cmd)

	require.DirExists(t, at, "but it is still the reviewer's writing")

	written, err := os.ReadFile(filepath.Join(at, "reviewer-scratch.txt"))
	require.NoError(t, err)
	assert.Equal(t, "what I want instead\n", string(written))
}

// TestReviewLeftoverIsNamedAtThePressThatTakesIt covers the warning landing where the destruction
// is: the notice from screen-open is cleared by the first keypress, and the press that checks the
// round out over the leftover used to be silent.
func TestReviewLeftoverIsNamedAtThePressThatTakesIt(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = pressEdit(t, m)
	at := m.review.roundDir
	writeRoundFile(t, m, "package greet\n\nfunc Hello() string {\n\treturn \"the reviewer's idea\"\n}\n")
	m = closeEditKey(t, m)

	left, cmd := m.leaveReview()
	drainCmd(cmd)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "greet.go"),
		[]byte("package greet\n\nfunc Hello() string {\n\treturn \"the agent moved on\"\n}\n"), 0o644))

	m = openReview(t, left.(model))

	// A keypress that reaches the diff clears the notice from screen-open, which is why the press
	// itself has to say what it is about to take.
	moved, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = moved.(model)
	require.Empty(t, m.review.notice)

	m = pressEdit(t, m)

	assert.Equal(t, at, m.review.replacing, "the press asked the disk and was refused")
	assert.Contains(t, m.review.notice, at, "it names what would go")
	assert.Contains(t, m.review.notice, "press again")
	assert.Equal(t, infoNothing, m.info.kind, "and no editor opened")
	assert.Empty(t, m.review.roundDir, "and nothing was checked out")

	held, err := os.ReadFile(filepath.Join(at, "greet.go"))
	require.NoError(t, err)
	assert.Contains(t, string(held), "the reviewer's idea", "the text is still there to be salvaged")

	// The second press is the answer, and it is what replaces the checkout.
	m = pressEdit(t, m)
	require.Equal(t, m.review.round.GetSnapshotSha(), m.review.roundSha)
	assert.Empty(t, m.review.replacing, "the warning is spent once it has been answered")

	held, err = os.ReadFile(filepath.Join(at, "greet.go"))
	require.NoError(t, err)
	assert.Contains(t, string(held), "the agent moved on", "which is the round the diff is drawing")
}

// TestReviewLeftoverIsCaughtByAPressThatBeatsTheLookup covers the reader pressing the editor key
// before the screen-open lookup has landed, which is why the refusal is asked for at the checkout
// rather than trusted from what was found earlier.
func TestReviewLeftoverIsCaughtByAPressThatBeatsTheLookup(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = pressEdit(t, m)
	at := m.review.roundDir
	writeRoundFile(t, m, "package greet\n\nfunc Hello() string {\n\treturn \"the reviewer's idea\"\n}\n")
	m = closeEditKey(t, m)

	left, cmd := m.leaveReview()
	drainCmd(cmd)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "greet.go"),
		[]byte("package greet\n\nfunc Hello() string {\n\treturn \"the agent moved on\"\n}\n"), 0o644))

	// The screen opens without the lookup ever being applied, which is the state a reader who
	// presses in the first moments after the diff paints is in.
	m = enterReviewWithoutPickup(t, left.(model))
	require.Empty(t, m.review.replacing)

	m = pressEdit(t, m)

	assert.Equal(t, at, m.review.replacing, "the press is still refused once")
	assert.Contains(t, m.review.notice, "press again")

	held, err := os.ReadFile(filepath.Join(at, "greet.go"))
	require.NoError(t, err)
	assert.Contains(t, string(held), "the reviewer's idea", "so the text is still there")
}

// TestReviewDrawsTheSidebarAloneWhileTheEditorIsUp covers the frame giving the window back: the
// panel is a column again, so squeezing the diff into it would leave neither half readable.
func TestReviewDrawsTheSidebarAloneWhileTheEditorIsUp(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	require.Contains(t, reviewFrameText(t, m), "package greet")

	m = pressEdit(t, m)

	_, ok := m.reviewSplit()
	assert.False(t, ok, "the window is shared with the editor")
	assert.NotContains(t, ansi.Strip(m.View()), "package greet", "so the diff waits")
	assert.Contains(t, ansi.Strip(m.View()), "Review · auth", "and the sidebar keeps the column")
}
