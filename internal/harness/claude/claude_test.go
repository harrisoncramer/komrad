package claude

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNamedConfigDir covers reading the directory the way the CLI does.
func TestNamedConfigDir(t *testing.T) {
	tests := []struct {
		name  string
		unset bool
		value string
		want  string
	}{
		{
			name:  "nothing when the variable is unset",
			unset: true,
		},
		{
			name:  "nothing when it is set but empty",
			value: "",
		},
		{
			name:  "the directory it names",
			value: "/logins/work",
			want:  "/logins/work",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(configDirEnv, tt.value)
			if tt.unset {
				require.NoError(t, os.Unsetenv(configDirEnv))
			}

			assert.Equal(t, tt.want, NamedConfigDir())
		})
	}
}

// TestLogPathFollowsTheConfigDir covers the log being read from under the directory the harness
// is rooted at.
func TestLogPathFollowsTheConfigDir(t *testing.T) {
	tests := []struct {
		name      string
		configDir string
		wantUnder func(home, configDir string) string
	}{
		{
			name:      "~/.claude when none is named",
			wantUnder: func(home, _ string) string { return filepath.Join(home, ".claude") },
		},
		{
			name:      "the named directory otherwise",
			configDir: "work login",
			wantUnder: func(_, configDir string) string { return configDir },
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

			dir := filepath.Join(tt.wantUnder(home, configDir), "projects", "-some-project")
			require.NoError(t, os.MkdirAll(dir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "sess-1.jsonl"), []byte("{}\n"), 0o600))

			h := NewHarness(NewHarnessParams{ConfigDir: configDir})
			assert.Equal(t, filepath.Join(dir, "sess-1.jsonl"), h.LogPath("sess-1"))
		})
	}
}
