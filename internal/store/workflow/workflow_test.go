package workflow

import (
	"testing"

	"komrad/internal/dbtest"
	"komrad/internal/store"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBlankBaseBranchIsRefused pins the database refusing a workflow that names no base, on the
// create and on an update.
func TestBlankBaseBranchIsRefused(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	_, err := st.Create(ctx, Config{Name: "backend"})
	require.ErrorContains(t, err, "worktree_base_branch_named")

	created, err := st.Create(ctx, Config{Name: "backend", Worktree: WorktreeConfig{BaseBranch: "main"}})
	require.NoError(t, err)

	err = st.Update(ctx, created.ID, Config{Name: "backend"})
	require.ErrorContains(t, err, "worktree_base_branch_named")
}

// TestWorktreeConfigRoundTrip checks a workflow's worktree config survives the store,
// including the nil-to-empty-array normalization a fresh Config's slices need against a
// column declared NOT NULL.
func TestWorktreeConfigRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		create Config
		update *Config
		want   WorktreeConfig
	}{
		{
			name:   "a config naming only its base stores disabled with empty lists",
			create: Config{Name: "backend", Worktree: WorktreeConfig{BaseBranch: "main"}},
			want: WorktreeConfig{
				BaseBranch:   "main",
				CopyPaths:    []string{},
				SymlinkPaths: []string{},
				DiffExclude:  []string{},
			},
		},
		{
			name: "every field is carried",
			create: Config{
				Name: "backend",
				Worktree: WorktreeConfig{
					Enabled:      true,
					BaseBranch:   "develop",
					Dir:          "/repo/.worktrees",
					SetupCommand: "mise install",
					CopyPaths:    []string{".env", "config/local.json"},
					SymlinkPaths: []string{"node_modules"},
					DiffExclude:  []string{"proto/gen", "*.pb.go"},
				},
			},
			want: WorktreeConfig{
				Enabled:      true,
				BaseBranch:   "develop",
				Dir:          "/repo/.worktrees",
				SetupCommand: "mise install",
				CopyPaths:    []string{".env", "config/local.json"},
				SymlinkPaths: []string{"node_modules"},
				DiffExclude:  []string{"proto/gen", "*.pb.go"},
			},
		},
		{
			name: "an update replaces the whole config, including turning it off",
			create: Config{
				Name: "backend",
				Worktree: WorktreeConfig{
					Enabled:    true,
					BaseBranch: "develop",
					CopyPaths:  []string{".env"},
				},
			},
			update: &Config{Name: "backend", Worktree: WorktreeConfig{BaseBranch: "develop"}},
			want: WorktreeConfig{
				BaseBranch:   "develop",
				CopyPaths:    []string{},
				SymlinkPaths: []string{},
				DiffExclude:  []string{},
			},
		},
		{
			name: "an update writes the directory worktrees are cut in and the command run in them",
			create: Config{
				Name:     "backend",
				Worktree: WorktreeConfig{BaseBranch: "main"},
			},
			update: &Config{
				Name: "backend",
				Worktree: WorktreeConfig{
					Enabled:      true,
					BaseBranch:   "main",
					Dir:          "/repo/.worktrees",
					SetupCommand: "mise install",
				},
			},
			want: WorktreeConfig{
				Enabled:      true,
				BaseBranch:   "main",
				Dir:          "/repo/.worktrees",
				SetupCommand: "mise install",
				CopyPaths:    []string{},
				SymlinkPaths: []string{},
				DiffExclude:  []string{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			created, err := st.Create(ctx, tt.create)
			require.NoError(t, err)

			if tt.update != nil {
				require.NoError(t, st.Update(ctx, created.ID, *tt.update))
			}

			got, err := st.Get(ctx, created.ID)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, tt.want, got.Worktree)

			listed, err := st.List(ctx)
			require.NoError(t, err)
			require.Len(t, listed, 1)
			assert.Equal(t, tt.want, listed[0].Worktree, "the listing carries it too")
		})
	}
}

var missingID = uuid.New()

func open(t *testing.T) *WorkflowStore {
	t.Helper()

	pool, err := store.Open(t.Context(), dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	return NewWorkflowStore(pool.GetDB())
}
