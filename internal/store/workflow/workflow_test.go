package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfigRoundTrip checks a workflow's cwd, flags, and context survive the
// join to workflow_configs, on every path that reads one back.
func TestConfigRoundTrip(t *testing.T) {
	cfg := Config{
		Name:     "backend",
		Context:  "shared notes",
		Cwd:      "/work/api",
		Flags:    "--model opus",
		Worktree: WorktreeConfig{BaseBranch: "main"},
	}

	tests := []struct {
		name        string
		update      *Config
		wantContext string
	}{
		{
			name:        "as created",
			wantContext: "shared notes",
		},
		{
			name: "after an update",
			update: &Config{
				Name:     "backend",
				Context:  "revised notes",
				Cwd:      "/work/api",
				Flags:    "--model opus",
				Worktree: WorktreeConfig{BaseBranch: "main"},
			},
			wantContext: "revised notes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			created, err := st.Create(ctx, cfg)
			require.NoError(t, err)
			assert.Equal(t, cfg.Cwd, created.Cwd, "the create result carries the config it was given")
			assert.Equal(t, cfg.Flags, created.Flags)

			if tt.update != nil {
				require.NoError(t, st.Update(ctx, created.ID, *tt.update))
			}

			got, err := st.Get(ctx, created.ID)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, tt.wantContext, got.Context)
			assert.Equal(t, cfg.Cwd, got.Cwd)
			assert.Equal(t, cfg.Flags, got.Flags)

			listed, err := st.List(ctx)
			require.NoError(t, err)
			require.Len(t, listed, 1)
			assert.Equal(t, tt.wantContext, listed[0].Context, "the listing joins the config too")
		})
	}
}

// TestWorktreeToggleRoundTrips pins the whole of what a workflow says about its agents'
// directories coming back as it was written.
func TestWorktreeToggleRoundTrips(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	created, err := st.Create(ctx, Config{
		Name:     "backend",
		Cwd:      "/repo",
		Worktree: WorktreeConfig{Enabled: true, BaseBranch: "develop"},
	})
	require.NoError(t, err)
	assert.True(t, created.Worktree.Enabled)
	assert.Equal(t, "develop", created.Worktree.BaseBranch)

	require.NoError(t, st.Update(ctx, created.ID, Config{Name: "backend", Cwd: "/repo", Worktree: WorktreeConfig{BaseBranch: "main"}}))

	got, err := st.Get(ctx, created.ID)
	require.NoError(t, err)
	assert.False(t, got.Worktree.Enabled, "an update writes the toggle like every other field")

	listed, err := st.List(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.False(t, listed[0].Worktree.Enabled, "and the listing carries it too")
}

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
					CopyPaths:    []string{".env", "config/local.json"},
					SymlinkPaths: []string{"node_modules"},
					DiffExclude:  []string{"proto/gen", "*.pb.go"},
				},
			},
			want: WorktreeConfig{
				Enabled:      true,
				BaseBranch:   "develop",
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
