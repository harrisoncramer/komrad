package workflow

import (
	"slices"
	"testing"

	"weave/internal/store"
	"weave/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
				created, err := st.Create(ctx, Config{Name: "backend", Worktree: WorktreeConfig{BaseBranch: "main"}})
				require.NoError(t, err)
				require.NotEmpty(t, created.ID, "an id is generated on create")
				id = created.ID
			}
			if tt.archive {
				require.NoError(t, st.Archive(ctx, id))
			}

			got, err := st.Get(ctx, id)

			if tt.wantName == "" {
				require.ErrorIs(t, err, store.ErrNotFound)

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
				created, err := st.Create(ctx, Config{Name: name, Worktree: WorktreeConfig{BaseBranch: "main"}})
				require.NoError(t, err)

				if name == tt.archive {
					require.NoError(t, st.Archive(ctx, created.ID))
				}
			}

			listed, err := st.List(ctx)
			require.NoError(t, err)

			names := xslice.Map(listed, func(workflow Workflow) string {
				return workflow.Name
			})
			assert.ElementsMatch(t, tt.wantNames, names)
		})
	}
}

// TestGetIncludingArchived pins that the read carries whether the workflow is archived rather than
// filtering on it.
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
				created, err := st.Create(ctx, Config{
					Name:     "backend",
					Cwd:      "/repo",
					Worktree: WorktreeConfig{BaseBranch: "main"},
				})
				require.NoError(t, err)
				id = created.ID
			}
			if tt.archive {
				require.NoError(t, st.Archive(ctx, id))
			}

			got, err := st.GetIncludingArchived(ctx, id)

			if !tt.wantHit {
				require.ErrorIs(t, err, store.ErrNotFound)

				return
			}

			require.NoError(t, err)

			require.NotNil(t, got)
			assert.Equal(t, "/repo", got.Cwd)

			if tt.archive {
				assert.NotNil(t, got.ArchivedAt)

				return
			}

			assert.Nil(t, got.ArchivedAt)
		})
	}
}

// TestListByIDSpansTheArchive pins that the lookup the assembler's page reads finds a workflow
// live or archived, with its provisioner config, and passes over an id nobody holds.
func TestListByIDSpansTheArchive(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	live, err := st.Create(ctx, Config{
		Name:     "live",
		Cwd:      "/repo",
		Worktree: WorktreeConfig{BaseBranch: "main"},
	})
	require.NoError(t, err)
	archived, err := st.Create(ctx, Config{
		Name:     "archived",
		Cwd:      "/repo",
		Worktree: WorktreeConfig{BaseBranch: "trunk"},
	})
	require.NoError(t, err)
	require.NoError(t, st.Archive(ctx, archived.ID))

	listed, err := st.ListByID(ctx, []string{live.ID, archived.ID, missingID, "not-an-id"})
	require.NoError(t, err)
	bases := xslice.KeyBy(listed, func(w Workflow) string { return w.Name })
	assert.Len(t, bases, 2)
	assert.Equal(t, "main", bases["live"].Worktree.BaseBranch)
	assert.Equal(t, "trunk", bases["archived"].Worktree.BaseBranch)

	none, err := st.ListByID(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, none)
}

// TestWatchedWorkflowsSpanTheArchive pins that the workflows the watch carries are the live and
// archived ones alike.
func TestWatchedWorkflowsSpanTheArchive(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	create := func(name string) *Workflow {
		created, err := st.Create(ctx, Config{
			Name:     name,
			Cwd:      "/repo",
			Worktree: WorktreeConfig{BaseBranch: "main"},
		})
		require.NoError(t, err)

		return created
	}
	live := create("live")
	archived := create("archived")
	require.NoError(t, st.Archive(ctx, archived.ID))
	names := func(workflows []Workflow) []string {
		return xslice.Map(workflows, func(w Workflow) string { return w.Name })
	}

	listed, err := st.GetWatched().List(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"archived", "live"}, names(listed))

	byKey, err := st.GetWatched().ListByKey(ctx, []string{live.ID, archived.ID, missingID})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"archived", "live"}, names(byKey))
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

			gone, err := st.Create(ctx, Config{Name: "archived", Worktree: WorktreeConfig{BaseBranch: "main"}})
			require.NoError(t, err)
			require.NoError(t, st.Archive(ctx, gone.ID))

			_, err = st.Create(ctx, Config{Name: "live", Worktree: WorktreeConfig{BaseBranch: "main"}})
			require.NoError(t, err)

			page, err := st.ListPage(ctx, 10, nil, tt.archived)
			require.NoError(t, err)

			names := xslice.Map(page, func(workflow Workflow) string {
				return workflow.Name
			})
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
		_, err := st.Create(ctx, Config{Name: name, Worktree: WorktreeConfig{BaseBranch: "main"}})
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

// TestListPageOrderMatchesByName pins that Go can reproduce the database's order, which the
// archive screen sorts a merged listing by.
func TestListPageOrderMatchesByName(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	var want []Workflow
	for _, name := range []string{"weavebot", "weave-bughunt", "weave", "Weave.Old", "weave_two"} {
		created, err := st.Create(ctx, Config{Name: name, Worktree: WorktreeConfig{BaseBranch: "main"}})
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
