package claude

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRelocate covers carrying a conversation to where the agent is about to be
// resumed. Claude resolves --resume against the folder named after the directory it
// runs in, so a restore that had to move the agent has to move the log with it.
func TestRelocate(t *testing.T) {
	const sessionID = "6f1b0c34-0b6b-4a17-9d1f-1f4b6c0e9a11"

	tests := []struct {
		name string
		// written places the log under the folder belonging to the original
		// directory, which is where claude wrote it while the agent ran there.
		written bool
		// sessionID is what the caller asks to be carried.
		sessionID string
		wantMoved bool
	}{
		{
			name:      "a log the agent still has follows it",
			written:   true,
			sessionID: sessionID,
			wantMoved: true,
		},
		{
			name:      "a conversation with no log on disk is nothing to move",
			sessionID: sessionID,
		},
		{
			name:    "an agent with no conversation is nothing to move",
			written: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projects := t.TempDir()
			from := filepath.Join(t.TempDir(), "worktree")
			to := filepath.Join(t.TempDir(), "stand-in")

			origin := filepath.Join(projects, projectFolder(from), sessionID+".jsonl")
			if tt.written {
				require.NoError(t, os.MkdirAll(filepath.Dir(origin), 0o755))
				require.NoError(t, os.WriteFile(origin, []byte("{}\n"), 0o600))
			}

			require.NoError(t, New(projects).Relocate(tt.sessionID, to))

			moved := filepath.Join(projects, projectFolder(to), sessionID+".jsonl")
			if !tt.wantMoved {
				_, err := os.Stat(moved)
				assert.ErrorIs(t, err, os.ErrNotExist, "nothing is written where there was nothing to carry")

				return
			}

			_, err := os.Stat(moved)
			require.NoError(t, err, "the conversation is filed under the directory the agent resumes in")

			_, err = os.Stat(origin)
			assert.ErrorIs(t, err, os.ErrNotExist, "the log is moved, so a glob on the session id finds one file")
		})
	}
}

// TestProjectFolder pins the encoding Claude uses to name the folder a
// directory's conversations live in, taken from the folders it has actually
// written. A wrong name here means a moved log a resume cannot find.
func TestProjectFolder(t *testing.T) {
	tests := []struct {
		dir  string
		want string
	}{
		{
			dir:  "/Users/someone/chariot/chariot/.worktrees/harry-fix-thing",
			want: "-Users-someone-chariot-chariot--worktrees-harry-fix-thing",
		},
		{
			dir:  "/Users/someone/weave",
			want: "-Users-someone-weave",
		},
	}

	for _, tt := range tests {
		t.Run(tt.dir, func(t *testing.T) {
			assert.Equal(t, tt.want, projectFolder(tt.dir))
		})
	}
}
