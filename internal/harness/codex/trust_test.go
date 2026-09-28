package codex

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTrustDir covers answering Codex's directory-trust question in its own config
// file, which belongs to the user so most cases are about not damaging it.
func TestTrustDir(t *testing.T) {
	tests := []struct {
		name        string
		existing    string
		dir         string
		wantTrusted string
		wantKept    string
		wantErr     string
	}{
		{
			name:        "the directory is recorded as trusted",
			dir:         "/repo/worktree",
			wantTrusted: "trusted",
		},
		{
			name:        "the rest of the user's config survives being appended to",
			existing:    "model = \"gpt-5.6\"\n\n[mcp_servers.theirs]\nurl = \"http://localhost:1234/mcp\"\n",
			dir:         "/repo/worktree",
			wantTrusted: "trusted",
			wantKept:    "[mcp_servers.theirs]",
		},
		{
			name:        "a directory already listed is left alone rather than written twice",
			existing:    "[projects.\"/repo/worktree\"]\ntrust_level = \"trusted\"\n",
			dir:         "/repo/worktree",
			wantTrusted: "trusted",
		},
		{
			name:        "a directory the user marked untrusted stays untrusted",
			existing:    "[projects.\"/repo/worktree\"]\ntrust_level = \"untrusted\"\n",
			dir:         "/repo/worktree",
			wantTrusted: "untrusted",
		},
		{
			name:     "a config that does not parse is reported rather than overwritten",
			existing: "this is not = = toml\n",
			dir:      "/repo/worktree",
			wantErr:  "failed to parse the codex config",
			wantKept: "this is not",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			path := filepath.Join(home, "config.toml")
			if tt.existing != "" {
				require.NoError(t, os.WriteFile(path, []byte(tt.existing), 0o600))
			}

			err := NewHarness(NewHarnessParams{Home: home}).TrustDir(tt.dir)

			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}

			written, err := os.ReadFile(path)
			require.NoError(t, err)

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
