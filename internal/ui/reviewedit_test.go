package ui

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEditorShellNamesTheLineTheEditorUnderstands covers the argument each editor is given to
// open on a line: an editor handed a flag it does not know takes it for a file name and opens
// an empty buffer under that name instead.
func TestEditorShellNamesTheLineTheEditorUnderstands(t *testing.T) {
	tests := []struct {
		name   string
		editor string
		want   string
	}{
		{
			name:   "nothing set falls back to vi",
			editor: "",
			want:   "exec vi +42 'greet.go'",
		},
		{
			name:   "an editor nobody here knows takes the terminal convention",
			editor: "kak",
			want:   "exec kak +42 'greet.go'",
		},
		{
			name:   "an editor of several words is read by the program it runs",
			editor: "nvim --clean",
			want:   "exec nvim --clean +42 'greet.go'",
		},
		{
			name:   "an editor named by its path is read by its base name",
			editor: "/opt/homebrew/bin/code",
			want:   "exec /opt/homebrew/bin/code --wait --goto 'greet.go:42'",
		},
		{
			name:   "code waits, since a window that returns at once reads as a closed editor",
			editor: "code",
			want:   "exec code --wait --goto 'greet.go:42'",
		},
		{
			name:   "zed takes the line on the path",
			editor: "zed",
			want:   "exec zed --wait 'greet.go:42'",
		},
		{
			name:   "helix takes the line on the path and needs no waiting",
			editor: "hx",
			want:   "exec hx 'greet.go:42'",
		},
		{
			name:   "a jetbrains launcher takes the line as its own flag",
			editor: "goland",
			want:   "exec goland --wait --line 42 'greet.go'",
		},
		{
			name:   "vi takes the line as a flag",
			editor: "vi",
			want:   "exec vi +42 'greet.go'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, editorShell(tt.editor, "greet.go", 42))
		})
	}
}

// TestEditorOpensAtTheRepositoryRoot covers an agent whose own directory is somewhere inside the
// repository: the diff names every file from the root, so opening one from the agent's directory
// names a file that is not there.
func TestEditorOpensAtTheRepositoryRoot(t *testing.T) {
	dir := reviewRepo(t)
	m, tmux := reviewModel(t, &fakeReviewAgents{}, dir)
	m.editor = "vi"
	m.listing.agents[0].agent.Cwd = filepath.Join(dir, "cmd", "greet")

	m = openReview(t, m)
	require.NotNil(t, m.review.diff.view)
	require.Equal(t, dir, m.review.root)

	written := pressEdit(t, m)
	require.True(t, written.review.pass.atEditor, "the edit key opened an editor")

	require.NotEmpty(t, tmux.respawnDirs)
	assert.Equal(t, dir, tmux.respawnDirs[0], "the editor is opened at the root the paths are named from")
	assert.Contains(t, tmux.respawns[0], "'greet.go'")
}
