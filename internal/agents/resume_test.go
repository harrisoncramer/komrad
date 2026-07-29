package agents

import (
	"os"
	"path/filepath"
	"testing"

	agentstore "claude-agents/internal/store/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResumeDir covers the directory a restore relaunches in. A restore has to
// produce a usable directory whatever became of the recorded one, since Claude
// resolves --resume against a folder named after the cwd and the panel lists
// agents whose worktree is long gone.
func TestResumeDir(t *testing.T) {
	const sessionID = "6f1b0c34-0b6b-4a17-9d1f-1f4b6c0e9a11"

	tests := []struct {
		name string
		// cwd builds the recorded working directory from a scratch root, and may
		// arrange for it to be missing or unmakeable.
		cwd func(t *testing.T, root string) string
		// wantDir derives the directory the restore should land in.
		wantDir func(root, restoreRoot string) string
		// wantMoved says the conversation log should have followed the agent into the
		// folder belonging to that directory.
		wantMoved bool
	}{
		{
			name: "a directory that is still there is used as it stands",
			cwd: func(t *testing.T, root string) string {
				dir := filepath.Join(root, "live")
				require.NoError(t, os.MkdirAll(dir, 0o755))

				return dir
			},
			wantDir: func(root, _ string) string { return filepath.Join(root, "live") },
		},
		{
			name: "a deleted worktree is recreated so the conversation stays reachable",
			cwd: func(_ *testing.T, root string) string {
				return filepath.Join(root, "repo", ".worktrees", "gone")
			},
			wantDir: func(root, _ string) string {
				return filepath.Join(root, "repo", ".worktrees", "gone")
			},
		},
		{
			name: "a path that cannot be remade sends the agent and its log to a stand-in",
			cwd: func(t *testing.T, root string) string {
				blocked := filepath.Join(root, "blocked")
				require.NoError(t, os.WriteFile(blocked, []byte("not a directory"), 0o600))

				return filepath.Join(blocked, "worktree")
			},
			wantDir:   func(_, restoreRoot string) string { return filepath.Join(restoreRoot, "agent-id") },
			wantMoved: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			projects := filepath.Join(t.TempDir(), "projects")
			restoreRoot := filepath.Join(t.TempDir(), "restored")

			cwd := tt.cwd(t, root)

			// The log starts out under the folder the original directory names, which is
			// where Claude wrote it while the agent was running there.
			origin := filepath.Join(projects, projectFolder(cwd))
			require.NoError(t, os.MkdirAll(origin, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(origin, sessionID+".jsonl"), []byte("{}\n"), 0o600))

			service := &AgentService{transcriptsDir: projects, restoreRoot: restoreRoot}
			got := service.resumeDir(agentstore.Agent{ID: "agent-id", Cwd: cwd, ClaudeSessionID: sessionID})

			assert.Equal(t, tt.wantDir(root, restoreRoot), got)
			require.NoError(t, usableDir(got), "a restore always gets a directory it can start in")

			moved := filepath.Join(projects, projectFolder(got), sessionID+".jsonl")
			_, err := os.Stat(moved)
			require.NoError(t, err, "the conversation is filed under the directory the agent resumes in")
			if tt.wantMoved {
				_, err := os.Stat(filepath.Join(origin, sessionID+".jsonl"))
				assert.ErrorIs(t, err, os.ErrNotExist, "the log is moved, so a glob on the session id finds one file")
			}
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
			dir:  "/Users/someone/tmux-claude-agents",
			want: "-Users-someone-tmux-claude-agents",
		},
	}

	for _, tt := range tests {
		t.Run(tt.dir, func(t *testing.T) {
			assert.Equal(t, tt.want, projectFolder(tt.dir))
		})
	}
}
