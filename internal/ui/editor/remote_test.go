package editor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRemoteEditorShell covers how each editor reaches a file on an engine on another machine.
func TestRemoteEditorShell(t *testing.T) {
	tests := []struct {
		name     string
		editor   string
		wantFile string
		wantDir  string
	}{
		{
			name:     "a terminal editor runs on the engine through its ssh host",
			editor:   "nvim",
			wantFile: "ssh -F '/home/me/.ssh/komrad/komrad-remote.conf' -t 'komrad-remote' 'exec nvim +42 '\\''/work/greet.go'\\''' || { printf '\\n%s\\n' 'the editor on the engine failed, press enter to close'; read -r _; }",
			wantDir:  "ssh -F '/home/me/.ssh/komrad/komrad-remote.conf' -t 'komrad-remote' 'exec nvim '\\''/work'\\''' || { printf '\\n%s\\n' 'the editor on the engine failed, press enter to close'; read -r _; }",
		},
		{
			name:     "a terminal editor named by its path here is found by its name there",
			editor:   "/opt/homebrew/bin/nvim --clean",
			wantFile: "ssh -F '/home/me/.ssh/komrad/komrad-remote.conf' -t 'komrad-remote' 'exec nvim --clean +42 '\\''/work/greet.go'\\''' || { printf '\\n%s\\n' 'the editor on the engine failed, press enter to close'; read -r _; }",
			wantDir:  "ssh -F '/home/me/.ssh/komrad/komrad-remote.conf' -t 'komrad-remote' 'exec nvim --clean '\\''/work'\\''' || { printf '\\n%s\\n' 'the editor on the engine failed, press enter to close'; read -r _; }",
		},
		{
			name:     "code opens its own window on the engine",
			editor:   "code",
			wantFile: "exec code --remote 'ssh-remote+komrad-remote' --wait --goto '/work/greet.go:42'",
			wantDir:  "exec code --remote 'ssh-remote+komrad-remote' '/work'",
		},
		{
			name:     "zed opens its own window on the engine, without the line",
			editor:   "zed",
			wantFile: "exec zed --wait 'ssh://komrad-remote/work/greet.go'",
			wantDir:  "exec zed 'ssh://komrad-remote/work'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := Settings{
				Command:   tt.editor,
				SSHHost:   "komrad-remote",
				SSHConfig: "/home/me/.ssh/komrad/komrad-remote.conf",
			}

			assert.Equal(t, tt.wantFile, remoteEditorShell(settings, "/work/greet.go", 42))
			assert.Equal(t, tt.wantDir, remoteDirEditorShell(settings, "/work"))
		})
	}
}
