package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"weave/internal/review"
	"weave/internal/ui/layout"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func editorKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")} }

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

// TestReviewEditorOpensTheFileUnderTheCursor covers the editor key: a split under the diff, the
// file the cursor is standing in opened on that line, and the keyboard handed to it.
func TestReviewEditorOpensTheFileUnderTheCursor(t *testing.T) {
	dir := reviewRepo(t)
	m, tmux := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	next, _ := m.updateReview(editorKey())
	m = next.(model)

	require.Equal(t, infoEditFile, m.info.kind)
	assert.Equal(t, []string{"%1"}, tmux.belowAnchors, "the split hangs off the stage")

	opened, rooted := lastRespawnOf(t, tmux, "%2")
	assert.Equal(t, m.review.root, rooted, "the editor is rooted in the worktree it is reading")
	assert.Contains(t, opened, "vi +1 'greet.go'")
	assert.Equal(t, []string{"%2"}, tmux.selected, "the editor is what the user types into")

	// The stage gave rows up to the split, so the diff is drawn again at what it is left with.
	assert.Equal(t, "weave stage "+reviewStagePath, tmux.respawns[len(tmux.respawns)-1])
}

// TestReviewEditorRunsTheConfiguredEditor covers an EDITOR of several words, which reaches the
// pane as the words it is made of rather than as the name of a program with spaces in it.
func TestReviewEditorRunsTheConfiguredEditor(t *testing.T) {
	dir := reviewRepo(t)
	m, tmux := reviewModel(t, &fakeReviewAgents{}, dir)
	m.editor = "nvim --clean"
	m = openReview(t, m)

	next, _ := m.updateReview(editorKey())
	m = next.(model)

	opened, _ := lastRespawnOf(t, tmux, "%2")
	assert.Contains(t, opened, "nvim --clean +1 'greet.go'")
}

// TestReviewEditorTogglesShut covers the second press: the same line is what opened the editor,
// so it is what takes it away.
func TestReviewEditorTogglesShut(t *testing.T) {
	dir := reviewRepo(t)
	m, tmux := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	next, _ := m.updateReview(editorKey())
	next, _ = next.(model).updateReview(editorKey())
	m = next.(model)

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

	next, _ := m.updateReview(editorKey())
	next, _ = next.(model).updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	next, _ = next.(model).updateReview(editorKey())
	m = next.(model)

	require.Equal(t, infoEditFile, m.info.kind)
	reopened, _ := lastRespawnOf(t, tmux, "%2")
	assert.Contains(t, reopened, "vi +5 'greet.go'")
}

// TestReviewEditCloseReloadsTheDiff pins both ways the editor closes reading the diff again,
// since the file was changed while it was up.
func TestReviewEditCloseReloadsTheDiff(t *testing.T) {
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
				next, cmd := m.reconcileEditor()

				return next, cmd
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := reviewRepo(t)
			m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
			m = openReview(t, m)

			next, _ := m.editReviewFile()
			m = next.(model)
			require.Equal(t, infoEditFile, m.info.kind, "the editor is up")

			require.NoError(t, os.WriteFile(filepath.Join(dir, "greet.go"),
				[]byte("package greet\n\nfunc Hello() string {\n\treturn \"edited in the editor\"\n}\n"), 0o644))

			closed, cmd := tt.close(m)
			m = closed.(model)
			require.NotNil(t, cmd, "closing the editor reads the worktree again")

			loaded, ok := cmd().(reviewLoadedMsg)
			require.True(t, ok)
			require.NoError(t, loaded.err)
			m.applyReviewLoaded(loaded)

			var drawn strings.Builder
			for y := range m.review.view.Len() {
				drawn.WriteString(m.review.view.RenderRow(y, review.NoCursor, 120, 0))
			}

			assert.Contains(t, drawn.String(), "edited in the editor", "the reloaded diff shows what the editor wrote")
		})
	}
}

// TestReviewEditCloseCaughtByTheTick pins the tick catching the editor gone as well, since a
// tmux that reports no focus event would otherwise leave the diff behind it stale.
func TestReviewEditCloseCaughtByTheTick(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	next, _ := m.editReviewFile()
	m = next.(model)
	require.Equal(t, infoEditFile, m.info.kind, "the editor is up")

	m.frame.Close(layout.RegionInfo)
	ticked, _ := m.route(refreshTickMsg{at: time.Now()})

	assert.Equal(t, infoNothing, ticked.(model).info.kind, "the tick noticed the editor gone")
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
