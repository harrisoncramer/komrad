package codex

import (
	"os"
	"path/filepath"
	"testing"

	"weave/internal/harness"

	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTrustLaunchDirs covers answering Codex's directory-trust question in its own config
// file, which belongs to the user so most cases are about not damaging it.
func TestTrustLaunchDirs(t *testing.T) {
	tests := []struct {
		name        string
		enabled     bool
		existing    string
		dir         string
		wantTrusted string
		wantKept    string
		wantErr     string
	}{
		{
			name:     "left off, the config is not touched and the agent meets the question itself",
			existing: "model = \"gpt-5.6\"\n",
			dir:      "/repo/worktree",
			wantKept: "model = \"gpt-5.6\"",
		},
		{
			name:        "turned on, the directory is recorded as trusted",
			enabled:     true,
			dir:         "/repo/worktree",
			wantTrusted: "trusted",
		},
		{
			name:        "the rest of the user's config survives being appended to",
			enabled:     true,
			existing:    "model = \"gpt-5.6\"\n\n[mcp_servers.theirs]\nurl = \"http://localhost:1234/mcp\"\n",
			dir:         "/repo/worktree",
			wantTrusted: "trusted",
			wantKept:    "[mcp_servers.theirs]",
		},
		{
			name:        "a directory already listed is left alone rather than written twice",
			enabled:     true,
			existing:    "[projects.\"/repo/worktree\"]\ntrust_level = \"trusted\"\n",
			dir:         "/repo/worktree",
			wantTrusted: "trusted",
		},
		{
			name:        "a directory the user marked untrusted stays untrusted",
			enabled:     true,
			existing:    "[projects.\"/repo/worktree\"]\ntrust_level = \"untrusted\"\n",
			dir:         "/repo/worktree",
			wantTrusted: "untrusted",
		},
		{
			name:     "a config that does not parse is reported rather than overwritten",
			enabled:  true,
			existing: "this is not = = toml\n",
			dir:      "/repo/worktree",
			wantErr:  "does not parse",
			wantKept: "this is not",
		},
		{
			name:    "no directory is nothing to record",
			enabled: true,
			dir:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if tt.existing != "" {
				require.NoError(t, os.WriteFile(path, []byte(tt.existing), 0o600))
			}

			harnessUnderTest := NewHarness(NewHarnessParams{
				SessionsDir:     t.TempDir(),
				ConfigPath:      path,
				TrustLaunchDirs: tt.enabled,
			})

			_, err := harnessUnderTest.BuildCommand(harness.Launch{
				Prompt:   "go",
				Dir:      tt.dir,
				CacheDir: t.TempDir(),
			})

			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}

			written, readErr := os.ReadFile(path)
			if tt.existing == "" && tt.wantTrusted == "" {
				assert.True(t, os.IsNotExist(readErr), "nothing is written when there is nothing to record")

				return
			}
			require.NoError(t, readErr)

			if tt.wantKept != "" {
				assert.Contains(t, string(written), tt.wantKept, "the user's own config survives")
			}

			assert.Equal(t, tt.wantTrusted, trustLevel(t, written, tt.dir))
		})
	}
}

func trustLevel(t *testing.T, config []byte, dir string) string {
	t.Helper()

	var parsed struct {
		Projects map[string]struct {
			TrustLevel string `toml:"trust_level"`
		} `toml:"projects"`
	}
	if err := toml.Unmarshal(config, &parsed); err != nil {
		return ""
	}

	return parsed.Projects[dir].TrustLevel
}
