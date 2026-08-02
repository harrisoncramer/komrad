package agents

import (
	"os"
	"path/filepath"
	"testing"

	agentstore "weave/internal/store/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResumeDir covers the directory a restore relaunches in, and whether the
// harness is asked to carry the conversation there. A restore has to produce a
// usable directory whatever became of the recorded one, since a harness may match
// a resume against the path the agent ran in and the panel lists agents whose
// worktree is long gone.
func TestResumeDir(t *testing.T) {
	const sessionID = "6f1b0c34-0b6b-4a17-9d1f-1f4b6c0e9a11"

	tests := []struct {
		name string
		// cwd builds the recorded working directory from a scratch root, and may
		// arrange for it to be missing or unmakeable.
		cwd func(t *testing.T, root string) string
		// wantDir derives the directory the restore should land in.
		wantDir func(root, fallbackResumeRoot string) string
		// wantMoved says the conversation should have been carried into the directory
		// the agent ends up in, which only a stand-in needs.
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
			wantDir:   func(_, fallbackResumeRoot string) string { return filepath.Join(fallbackResumeRoot, "agent-id") },
			wantMoved: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			fallbackResumeRoot := filepath.Join(t.TempDir(), "restored")

			cwd := tt.cwd(t, root)

			h := &fakeHarness{}
			service := &AgentService{fallbackResumeRoot: fallbackResumeRoot}
			got := service.getResumeDir(t.Context(), agentstore.Agent{ID: "agent-id", Cwd: cwd, SessionID: sessionID}, h)

			assert.Equal(t, tt.wantDir(root, fallbackResumeRoot), got)
			require.NoError(t, requireUsableDir(got), "a restore always gets a directory it can start in")

			if !tt.wantMoved {
				assert.Empty(t, h.relocated, "a directory the agent can be started in leaves the conversation alone")

				return
			}

			assert.Equal(t, [][2]string{{sessionID, got}}, h.relocated,
				"the conversation is carried into the directory the agent resumes in")
		})
	}
}
