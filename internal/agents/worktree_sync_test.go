package agents

import (
	"os"
	"path/filepath"
	"testing"

	agentstore "weave/internal/store/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWorktreeSyncerSync covers what the periodic pass writes for a live agent: whether its
// cwd still exists on disk.
func TestWorktreeSyncerSync(t *testing.T) {
	tests := []struct {
		name string
		cwd  string
		want bool
	}{
		{
			name: "a blank cwd is not reported removed",
			cwd:  "",
			want: false,
		},
		{
			name: "a cwd that still exists is not reported removed",
			cwd:  t.TempDir(),
			want: false,
		},
		{
			name: "a cwd that no longer exists is reported removed",
			cwd:  removedDir(t),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			ctx := t.Context()

			workflowID := seedWorkflow(t, st)
			require.NoError(t, st.Agents.Create(ctx, agentstore.Agent{Name: "a", WorkflowID: workflowID, Cwd: tt.cwd}))
			created, err := st.Agents.GetInWorkflow(ctx, workflowID, "a")
			require.NoError(t, err)

			require.NoError(t, NewWorktreeSyncer(st.Agents).Sync(ctx))

			got, err := st.Agents.GetByID(ctx, created.ID)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, tt.want, got.WorktreeRemoved)
		})
	}
}

// TestWorktreeSyncerSyncClearsAStaleRemoval covers an agent whose cwd was recreated after
// vanishing: the next pass has to clear what an earlier pass wrote rather than leave it
// stuck as removed.
func TestWorktreeSyncerSyncClearsAStaleRemoval(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	require.NoError(t, st.Agents.Create(ctx, agentstore.Agent{Name: "a", WorkflowID: workflowID, Cwd: removedDir(t)}))
	created, err := st.Agents.GetInWorkflow(ctx, workflowID, "a")
	require.NoError(t, err)

	syncer := NewWorktreeSyncer(st.Agents)
	require.NoError(t, syncer.Sync(ctx))

	got, err := st.Agents.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.True(t, got.WorktreeRemoved)

	got.Cwd = t.TempDir()
	require.NoError(t, st.Agents.Update(ctx, *got))

	require.NoError(t, syncer.Sync(ctx))

	after, err := st.Agents.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.False(t, after.WorktreeRemoved)
}

// TestWorktreeSyncerSyncSkipsArchivedAgents covers that the pass walks live agents only,
// since an archived one's cwd is expected to be gone and there is nothing left to show a
// removed glyph beside.
func TestWorktreeSyncerSyncSkipsArchivedAgents(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	require.NoError(t, st.Agents.Create(ctx, agentstore.Agent{Name: "a", WorkflowID: workflowID, Cwd: removedDir(t)}))
	created, err := st.Agents.GetInWorkflow(ctx, workflowID, "a")
	require.NoError(t, err)
	require.NoError(t, st.Agents.Archive(ctx, []string{created.ID}))

	require.NoError(t, NewWorktreeSyncer(st.Agents).Sync(ctx))

	got, err := st.Agents.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.False(t, got.WorktreeRemoved, "an archived agent is not walked, so nothing writes a removal for it")
}

// removedDir returns a path inside a directory the test framework will clean up, deleted
// immediately so it names no directory a syncer's stat call can find.
func removedDir(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "gone")
	require.NoError(t, os.Mkdir(dir, 0o755))
	require.NoError(t, os.Remove(dir))

	return dir
}
