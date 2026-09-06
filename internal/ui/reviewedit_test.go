package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

	// The stage gave rows up to the split, so the diff is drawn again at what it is left with.
	assert.Equal(t, "weave stage "+reviewStagePath, tmux.respawns[len(tmux.respawns)-1])
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

	next, _ := m.closeReviewEdit()
	m = next.(model)

	worktree, err := os.ReadFile(filepath.Join(dir, "greet.go"))
	require.NoError(t, err)

	assert.NotContains(t, string(worktree), "the reviewer's idea", "the agent's file is untouched")
	require.NotNil(t, m.review.suggestion, "and the edit is held as a suggestion instead")
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

				return m.reconcileEditor(), nil
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

			closed, _ := tt.close(m)
			m = closed.(model)

			held := m.review.suggestion
			require.NotNil(t, held, "the edit is held as a suggestion")
			assert.Equal(t, "greet.go", held.anchor.FilePath)
			assert.Equal(t, 4, held.anchor.NewStart)
			assert.Equal(t, 4, held.anchor.NewEnd)
			assert.Equal(t, []string{"\treturn \"edited in the editor\""}, held.lines)
		})
	}
}

// TestReviewEditRevertedDropsTheSuggestion covers the reviewer changing their mind in the editor:
// a file put back the way the round holds it proposes nothing.
func TestReviewEditRevertedDropsTheSuggestion(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = pressEdit(t, m)
	writeRoundFile(t, m, "package greet\n\nfunc Hello() string {\n\treturn \"an idea\"\n}\n")

	next, _ := m.closeReviewEdit()
	m = next.(model)
	require.NotNil(t, m.review.suggestion)

	m = pressEdit(t, m)
	writeRoundFile(t, m, "package greet\n\nfunc Hello() string {\n\treturn \"goodbye\"\n}\n")

	next, _ = m.closeReviewEdit()

	assert.Nil(t, next.(model).review.suggestion, "putting the file back takes the suggestion with it")
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

	next, _ := m.closeReviewEdit()
	m = next.(model)

	shown := ansi.Strip(m.reviewView())
	assert.Contains(t, shown, "edit held for greet.go:4", "the sidebar says where it will land")

	next, _ = m.updateReview(composeKey())
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
	assert.Nil(t, m.review.suggestion, "and the panel is not still holding it")
}

// TestReviewSuggestionDroppedOnCancel covers the composer being the place weave asks whether to
// send an edit, so leaving it without writing anything is the answer.
func TestReviewSuggestionDroppedOnCancel(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m = pressEdit(t, m)
	writeRoundFile(t, m, "package greet\n\nfunc Hello() string {\n\treturn \"farewell\"\n}\n")

	next, _ := m.closeReviewEdit()
	next, _ = next.(model).updateReview(composeKey())
	next, _ = next.(model).updateReview(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)

	assert.Nil(t, m.review.suggestion)
	assert.Nil(t, agents.created, "nothing was sent")
	assert.Contains(t, m.review.notice, "dropped")
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

// TestReviewKeepsDrawingAfterAReload pins the diff repainting after a reload, since a reload
// that drops the pane measurement leaves it frozen while the sidebar keeps moving.
func TestReviewKeepsDrawingAfterAReload(t *testing.T) {
	dir := reviewRepo(t)
	m, tmux := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	reloaded, cmd := m.reloadReview()
	require.NotNil(t, cmd)

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)
	require.NoError(t, loaded.err)

	m = reloaded
	m.applyReviewLoaded(loaded)

	require.NotZero(t, m.review.frames.width, "the stage is still measured")

	before := len(tmux.sentKeys)
	moved, _ := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})

	assert.Greater(t, len(tmux.sentKeys), before, "the move tells the drawer to read the new screenful")
	assert.NotEqual(t, m.review.cursor, moved.(model).review.cursor, "and the cursor actually moved")
}

// TestReviewEditReadsUnchangedFileQuietly covers closing the editor without having saved
// anything, which proposes nothing and says nothing.
func TestReviewEditReadsUnchangedFileQuietly(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m = pressEdit(t, m)

	next, _ := m.closeReviewEdit()
	m = next.(model)

	assert.Nil(t, m.review.suggestion)
	assert.Empty(t, strings.TrimSpace(m.review.notice))
}
