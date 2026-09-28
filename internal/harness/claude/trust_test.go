package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTrustDir covers answering Claude's directory-trust question in its own state file,
// which belongs to the user so most cases are about not damaging it.
func TestTrustDir(t *testing.T) {
	tests := []struct {
		name        string
		existing    string
		dir         string
		wantTrusted bool
		wantKept    string
		wantErr     string
	}{
		{
			name:        "the directory is recorded as trusted",
			dir:         "/repo/worktree",
			wantTrusted: true,
		},
		{
			name:        "the rest of the user's state survives",
			existing:    `{"userID":"abc","projects":{"/elsewhere":{"allowedTools":["Bash"],"hasTrustDialogAccepted":true}}}`,
			dir:         "/repo/worktree",
			wantTrusted: true,
			wantKept:    `"allowedTools"`,
		},
		{
			name:        "a project Claude already knows keeps its own fields",
			existing:    `{"projects":{"/repo/worktree":{"lastCost":1.5,"hasTrustDialogAccepted":false}}}`,
			dir:         "/repo/worktree",
			wantTrusted: true,
			wantKept:    `"lastCost"`,
		},
		{
			name:        "a directory already trusted is left as it was written",
			existing:    `{"projects":{"/repo/worktree":{"hasTrustDialogAccepted":true}}}`,
			dir:         "/repo/worktree",
			wantTrusted: true,
			wantKept:    `{"projects":{"/repo/worktree":{"hasTrustDialogAccepted":true}}}`,
		},
		{
			name:     "a state file that does not parse is reported rather than overwritten",
			existing: "not json",
			dir:      "/repo/worktree",
			wantErr:  "failed to parse the claude state",
			wantKept: "not json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configDir := t.TempDir()
			path := filepath.Join(configDir, ".claude.json")
			if tt.existing != "" {
				require.NoError(t, os.WriteFile(path, []byte(tt.existing), 0o600))
			}

			err := NewHarness(NewHarnessParams{ConfigDir: configDir}).TrustDir(tt.dir)

			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}

			written, err := os.ReadFile(path)
			require.NoError(t, err)

			if tt.wantKept != "" {
				assert.Contains(t, string(written), tt.wantKept)
			}

			assert.Equal(t, tt.wantTrusted, isTrusted(t, written, tt.dir))
		})
	}
}

// TestTrustDirFindsTheStateBesideHomeByDefault covers a CLI left to its default config dir.
func TestTrustDirFindsTheStateBesideHomeByDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	require.NoError(t, NewHarness(NewHarnessParams{}).TrustDir("/repo/worktree"))

	written, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	require.NoError(t, err)
	assert.True(t, isTrusted(t, written, "/repo/worktree"))
}

func isTrusted(t *testing.T, state []byte, dir string) bool {
	t.Helper()

	var parsed struct {
		Projects map[string]struct {
			HasTrustDialogAccepted bool `json:"hasTrustDialogAccepted"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(state, &parsed); err != nil {
		return false
	}

	return parsed.Projects[dir].HasTrustDialogAccepted
}
