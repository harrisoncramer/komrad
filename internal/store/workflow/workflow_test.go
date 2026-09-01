package workflow

import (
	"slices"
	"testing"

	"weave/internal/pgtest"
	"weave/internal/store"
	"weave/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const missingID = "00000000-0000-0000-0000-000000000000"

// TestGet pins which lookups find a workflow, an archived one and an id nobody holds
// included.
func TestGet(t *testing.T) {
	tests := []struct {
		name     string
		create   bool
		archive  bool
		id       string
		wantName string
	}{
		{
			name: "an id nobody holds",
			id:   missingID,
		},
		{
			name:    "an archived workflow is hidden",
			create:  true,
			archive: true,
		},
		{
			name:     "a workflow that exists",
			create:   true,
			wantName: "backend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			id := tt.id
			if tt.create {
				created, err := st.Create(ctx, Config{Name: "backend"})
				require.NoError(t, err)
				require.NotEmpty(t, created.ID, "an id is generated on create")
				id = created.ID
			}
			if tt.archive {
				require.NoError(t, st.Archive(ctx, id))
			}

			got, err := st.Get(ctx, id)

			if tt.wantName == "" {
				assert.ErrorIs(t, err, store.ErrNotFound)

				return
			}

			require.NotNil(t, got)
			assert.Equal(t, tt.wantName, got.Name)
		})
	}
}

// TestList pins what a listing holds, and that an archived workflow drops out of it.
func TestList(t *testing.T) {
	tests := []struct {
		name      string
		create    []string
		archive   string
		wantNames []string
	}{
		{
			name: "an empty store lists nothing",
		},
		{
			name:      "an archived workflow drops out of the listing",
			create:    []string{"backend", "frontend"},
			archive:   "backend",
			wantNames: []string{"frontend"},
		},
		{
			name:      "every live workflow is listed",
			create:    []string{"backend", "frontend"},
			wantNames: []string{"backend", "frontend"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			for _, name := range tt.create {
				created, err := st.Create(ctx, Config{Name: name})
				require.NoError(t, err)

				if name == tt.archive {
					require.NoError(t, st.Archive(ctx, created.ID))
				}
			}

			listed, err := st.List(ctx)
			require.NoError(t, err)

			names := make([]string, 0, len(listed))
			for _, workflow := range listed {
				names = append(names, workflow.Name)
			}
			assert.ElementsMatch(t, tt.wantNames, names)
		})
	}
}

// TestRename pins that a rename cannot collide, since nothing keys on a workflow's name the
// way it does on an agent's.
func TestRename(t *testing.T) {
	tests := []struct {
		name   string
		create bool
		to     string
		want   string
	}{
		{
			name: "an id nobody holds is not an error",
			to:   "ghost",
		},
		{
			name:   "a live workflow takes the new name",
			create: true,
			to:     "platform",
			want:   "platform",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			id := missingID
			if tt.create {
				created, err := st.Create(ctx, Config{Name: "backend"})
				require.NoError(t, err)
				id = created.ID
			}

			require.NoError(t, st.Rename(ctx, id, tt.to))

			if tt.want == "" {
				return
			}

			got, err := st.Get(ctx, id)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, tt.want, got.Name)
		})
	}
}

// TestConfigRoundTrip checks a workflow's cwd, flags, and context survive the
// join to workflow_configs, on every path that reads one back.
func TestConfigRoundTrip(t *testing.T) {
	cfg := Config{
		Name:    "backend",
		Context: "shared notes",
		Cwd:     "/work/api",
		Flags:   "--model opus",
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
				Name:    "backend",
				Context: "revised notes",
				Cwd:     "/work/api",
				Flags:   "--model opus",
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

// TestActionsRoundTrip pins that the setup and cleanup actions come back as they were
// written, naming none storing blank.
func TestActionsRoundTrip(t *testing.T) {
	tests := []struct {
		name        string
		create      Config
		update      *Config
		wantSetup   string
		wantCleanup string
	}{
		{
			name:   "naming none stores blank",
			create: Config{Name: "digest"},
		},
		{
			name: "both are carried",
			create: Config{
				Name:          "backend",
				SetupAction:   "make-worktree",
				CleanupAction: "drop-worktree",
			},
			wantSetup:   "make-worktree",
			wantCleanup: "drop-worktree",
		},
		{
			name: "an update writes both, so clearing one clears it",
			create: Config{
				Name:          "backend",
				SetupAction:   "make-worktree",
				CleanupAction: "drop-worktree",
			},
			update:    &Config{Name: "backend", SetupAction: "provision"},
			wantSetup: "provision",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			created, err := st.Create(ctx, tt.create)
			require.NoError(t, err)
			assert.Equal(t, tt.create.SetupAction, created.SetupAction, "the create result carries what it was given")

			if tt.update != nil {
				require.NoError(t, st.Update(ctx, created.ID, *tt.update))
			}

			got, err := st.Get(ctx, created.ID)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, tt.wantSetup, got.SetupAction)
			assert.Equal(t, tt.wantCleanup, got.CleanupAction)

			listed, err := st.List(ctx)
			require.NoError(t, err)
			require.Len(t, listed, 1)
			assert.Equal(t, tt.wantSetup, listed[0].SetupAction, "the listing carries them too")
		})
	}
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
			name:   "an empty config stores disabled with empty lists",
			create: Config{Name: "backend"},
			want: WorktreeConfig{
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
			update: &Config{Name: "backend"},
			want: WorktreeConfig{
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

func open(t *testing.T) *Store {
	t.Helper()

	pool, err := store.Open(t.Context(), pgtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	return New(pool.GetDB())
}

// TestGetIncludingArchived pins that the read carries whether the workflow is archived rather
// than filtering on it, since that is what a restore is refused by.
func TestGetIncludingArchived(t *testing.T) {
	tests := []struct {
		name    string
		create  bool
		archive bool
		wantHit bool
	}{
		{
			name: "an id nobody holds",
		},
		{
			name:    "a live workflow",
			create:  true,
			wantHit: true,
		},
		{
			name:    "an archived workflow",
			create:  true,
			archive: true,
			wantHit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			id := missingID
			if tt.create {
				created, err := st.Create(ctx, Config{Name: "backend", CleanupAction: "drop-worktree"})
				require.NoError(t, err)
				id = created.ID
			}
			if tt.archive {
				require.NoError(t, st.Archive(ctx, id))
			}

			got, err := st.GetIncludingArchived(ctx, id)

			if !tt.wantHit {
				assert.ErrorIs(t, err, store.ErrNotFound)

				return
			}

			require.NoError(t, err)

			require.NotNil(t, got)
			assert.Equal(t, "drop-worktree", got.CleanupAction)

			if tt.archive {
				assert.NotNil(t, got.ArchivedAt)

				return
			}

			assert.Nil(t, got.ArchivedAt)
		})
	}
}

// TestListPageArchived pins that a workflow in one listing never shows up in the other, which
// is what the panel and its archive each draw their headers from.
func TestListPageArchived(t *testing.T) {
	tests := []struct {
		name      string
		archived  bool
		wantNames []string
	}{
		{
			name:      "the live listing leaves out what was archived",
			wantNames: []string{"live"},
		},
		{
			name:      "the archive listing is the archived rows and only those",
			archived:  true,
			wantNames: []string{"archived"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			gone, err := st.Create(ctx, Config{Name: "archived"})
			require.NoError(t, err)
			require.NoError(t, st.Archive(ctx, gone.ID))

			_, err = st.Create(ctx, Config{Name: "live"})
			require.NoError(t, err)

			page, err := st.ListPage(ctx, 10, nil, tt.archived)
			require.NoError(t, err)

			names := make([]string, 0, len(page))
			for _, workflow := range page {
				names = append(names, workflow.Name)
			}
			assert.Equal(t, tt.wantNames, names)

			if !tt.archived {
				assert.Nil(t, page[0].ArchivedAt, "a live workflow carries no archive time")

				return
			}

			assert.NotNil(t, page[0].ArchivedAt, "and an archived one carries when it was put away")
		})
	}
}

// TestListPageIsAlphabetical covers the order the panel reads workflows in across a page
// boundary, where the id keeps two workflows sharing a name from serving the same row twice.
func TestListPageIsAlphabetical(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	for _, name := range []string{"zebra", "backend", "backend", "Api", "frontend"} {
		_, err := st.Create(ctx, Config{Name: name})
		require.NoError(t, err)
	}

	var (
		names  []string
		ids    = map[string]bool{}
		cursor *Cursor
	)
	for range 10 {
		page, err := st.ListPage(ctx, 2, cursor, false)
		require.NoError(t, err)
		if len(page) == 0 {
			break
		}

		for _, workflow := range page {
			assert.False(t, ids[workflow.ID], "a workflow appears on one page only")
			ids[workflow.ID] = true
			names = append(names, workflow.Name)
		}

		last := page[len(page)-1]
		cursor = &Cursor{Name: last.Name, ID: last.ID}
	}

	assert.Equal(t, []string{"Api", "backend", "backend", "frontend", "zebra"}, names)
}

// TestListPageOrderMatchesByName pins that Go can reproduce the database's order, since the
// archive screen sorts a merged listing itself and punctuation is where the two used to part.
func TestListPageOrderMatchesByName(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	var want []Workflow
	for _, name := range []string{"weavebot", "weave-bughunt", "weave", "Weave.Old", "weave_two"} {
		created, err := st.Create(ctx, Config{Name: name})
		require.NoError(t, err)
		want = append(want, *created)
	}
	slices.SortStableFunc(want, ByName)

	var (
		got    []Workflow
		cursor *Cursor
	)
	for range 10 {
		page, err := st.ListPage(ctx, 2, cursor, false)
		require.NoError(t, err)
		if len(page) == 0 {
			break
		}

		got = append(got, page...)

		last := page[len(page)-1]
		cursor = &Cursor{Name: last.Name, ID: last.ID}
	}

	assert.Equal(t, xslice.Map(want, func(w Workflow) string { return w.Name }),
		xslice.Map(got, func(w Workflow) string { return w.Name }))
}

// TestDelete pins that a hard delete keyed by id takes the config rows a workflow alone owns
// with it.
func TestDelete(t *testing.T) {
	tests := []struct {
		name   string
		create bool
	}{
		{
			name: "an id nobody holds is not an error",
		},
		{
			name:   "a workflow row and its config rows are all removed",
			create: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool, err := store.Open(t.Context(), pgtest.URL(t))
			require.NoError(t, err)
			t.Cleanup(func() { _ = pool.Close() })

			st := New(pool.GetDB())
			ctx := t.Context()

			id := missingID
			if tt.create {
				created, err := st.Create(ctx, Config{Name: "backend"})
				require.NoError(t, err)
				id = created.ID
			}

			require.NoError(t, st.Delete(ctx, id))

			_, err = st.GetIncludingArchived(ctx, id)
			require.ErrorIs(t, err, store.ErrNotFound, "the row is gone")

			if !tt.create {
				return
			}

			var configs, actionConfigs int
			require.NoError(t, pool.GetRawPool().QueryRow(ctx, "SELECT count(*) FROM workflow_configs").Scan(&configs))
			require.NoError(t, pool.GetRawPool().QueryRow(ctx, "SELECT count(*) FROM workflow_action_configs").Scan(&actionConfigs))
			assert.Zero(t, configs, "its config row goes with it")
			assert.Zero(t, actionConfigs, "and its action config row too")
		})
	}
}
