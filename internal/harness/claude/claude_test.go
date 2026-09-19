package claude

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultConfigDir covers the directory resolving the way the CLI resolves it, so a login
// kept outside ~/.claude writes its log where weave reads.
func TestDefaultConfigDir(t *testing.T) {
	tests := []struct {
		name      string
		configDir string
		want      func(home, configDir string) string
	}{
		{
			name: "the home directory when nothing says otherwise",
			want: func(home, _ string) string { return filepath.Join(home, ".claude") },
		},
		{
			name:      "CLAUDE_CONFIG_DIR when it is set",
			configDir: "work login",
			want:      func(_, configDir string) string { return configDir },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)

			configDir := ""
			if tt.configDir != "" {
				configDir = filepath.Join(t.TempDir(), tt.configDir)
			}
			t.Setenv(configDirEnv, configDir)

			want := tt.want(home, configDir)
			assert.Equal(t, want, DefaultConfigDir())

			dir := filepath.Join(want, "projects", "-some-project")
			require.NoError(t, os.MkdirAll(dir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "sess-1.jsonl"), []byte("{}\n"), 0o600))

			h := NewHarness(NewHarnessParams{ConfigDir: DefaultConfigDir()})
			assert.Equal(t, filepath.Join(dir, "sess-1.jsonl"), h.LogPath("sess-1"),
				"the log the CLI wrote there is the one weave reads")
		})
	}
}
