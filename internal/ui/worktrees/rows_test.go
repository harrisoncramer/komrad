package worktrees

import (
	"testing"
	"time"

	"komrad/internal/glyph"
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/tabs"
	"komrad/internal/view"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWorktreeRowsOrderEachGroupByTheColumn covers the headers staying in path order under
// every sort, the rows under each ordered by the chosen column in the arrow's direction with
// the rows that lack the value at the bottom, and the directory name settling a tie.
func TestWorktreeRowsOrderEachGroupByTheColumn(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	items := measuredFixture(now)
	items[0].Size.Bytes = 60 * 1024 * 1024

	tests := []struct {
		name  string
		items []panel.Worktree
		state tabs.State
		want  []string
	}{
		{
			name:  "nothing measured falls to the directory name",
			items: worktreeFixture(),
			state: tabs.State{Chosen: "size", Direction: tabs.Up},
			want: []string{
				"repo:/home/h/gadgets/admin",
				"tree:/trees/admin",
				"repo:/home/h/komrad",
				"tree:/trees/archived",
				"tree:/trees/live",
				"tree:/trees/orphan",
				"repo:/var/folders/T/repo",
				"tree:/trees/broken",
			},
		},
		{
			name:  "size down is the fallback, largest first and the failed walk last",
			items: items,
			state: tabs.State{},
			want: []string{
				"repo:/home/h/gadgets/admin",
				"tree:/trees/admin",
				"repo:/home/h/komrad",
				"tree:/trees/live",
				"tree:/trees/archived",
				"tree:/trees/orphan",
				"repo:/var/folders/T/repo",
				"tree:/trees/broken",
			},
		},
		{
			name:  "size up is smallest first with the failed walk still last",
			items: items,
			state: tabs.State{Chosen: "size", Direction: tabs.Up},
			want: []string{
				"repo:/home/h/gadgets/admin",
				"tree:/trees/admin",
				"repo:/home/h/komrad",
				"tree:/trees/archived",
				"tree:/trees/live",
				"tree:/trees/orphan",
				"repo:/var/folders/T/repo",
				"tree:/trees/broken",
			},
		},
		{
			name:  "last-accessed down is the most recent first, the failed walk placed by its own activity",
			items: items,
			state: tabs.State{Chosen: "last-accessed", Direction: tabs.Down},
			want: []string{
				"repo:/home/h/gadgets/admin",
				"tree:/trees/admin",
				"repo:/home/h/komrad",
				"tree:/trees/live",
				"tree:/trees/orphan",
				"tree:/trees/archived",
				"repo:/var/folders/T/repo",
				"tree:/trees/broken",
			},
		},
		{
			name:  "last-accessed up is the coldest first",
			items: items,
			state: tabs.State{Chosen: "last-accessed", Direction: tabs.Up},
			want: []string{
				"repo:/home/h/gadgets/admin",
				"tree:/trees/admin",
				"repo:/home/h/komrad",
				"tree:/trees/archived",
				"tree:/trees/orphan",
				"tree:/trees/live",
				"repo:/var/folders/T/repo",
				"tree:/trees/broken",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := buildRows(buildRowsParams{items: tt.items, order: worktreesOrder(tt.state)})

			assert.Equal(t, tt.want, xslice.Map(rows, row.Key))
		})
	}
}

// TestWorktreeRowsFlatOrderTheWholeListing covers the headings going and every directory ordered
// as one list by the chosen column in the arrow's direction, the rows that lack the value at the
// bottom, and no row carrying a repository.
func TestWorktreeRowsFlatOrderTheWholeListing(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	items := measuredFixture(now)
	items[0].Size.Bytes = 60 * 1024 * 1024

	tests := []struct {
		name  string
		state tabs.State
		want  []string
	}{
		{
			name:  "size down is the largest first across every repository",
			state: tabs.State{Flat: true},
			want:  []string{"tree:/trees/admin", "tree:/trees/live", "tree:/trees/archived", "tree:/trees/broken", "tree:/trees/orphan"},
		},
		{
			name: "size up is the smallest first with the unmeasured still last",
			state: tabs.State{
				Chosen:    "size",
				Direction: tabs.Up,
				Flat:      true,
			},
			want: []string{"tree:/trees/archived", "tree:/trees/live", "tree:/trees/admin", "tree:/trees/broken", "tree:/trees/orphan"},
		},
		{
			name:  "last-accessed down is the most recent first",
			state: tabs.State{Chosen: "last-accessed", Flat: true},
			want:  []string{"tree:/trees/live", "tree:/trees/orphan", "tree:/trees/admin", "tree:/trees/archived", "tree:/trees/broken"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := buildRows(buildRowsParams{
				items: items,
				order: worktreesOrder(tt.state),
				flat:  true,
			})

			assert.Equal(t, tt.want, xslice.Map(rows, row.Key))
			for _, row := range rows {
				assert.False(t, row.header)
				assert.Empty(t, row.repo)
			}
		})
	}
}

// TestWorktreesSnapshotRendersFlatRowsAtTheHeadingIndent covers a flat row being a result rather
// than an agent under a heading, with its cells as they were.
func TestWorktreesSnapshotRendersFlatRowsAtTheHeadingIndent(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	items := measuredFixture(now)
	snapshot := listSnapshot(listSnapshotParams{
		rows: buildRows(buildRowsParams{
			items: items,
			order: worktreesOrder(tabs.State{}),
			flat:  true,
		}),
		items: items,
		flat:  true,
		now:   now,
	})

	require.Len(t, snapshot.Rows, 5)
	for _, row := range snapshot.Rows {
		assert.Equal(t, view.KindResult, row.Kind)
	}
	assert.Equal(t, []view.Cell{{Text: "1.5G", Tone: paint.ToneMuted}, {Text: "12h", Tone: paint.ToneMuted}, {Glyph: glyph.Merged, Tone: paint.ToneMerged}}, snapshot.Rows[0].Cells)
}

// TestWorktreeRowsLeaveTheOccupantOffTheList covers an agent working in a task's directory
// getting no row of its own.
func TestWorktreeRowsLeaveTheOccupantOffTheList(t *testing.T) {
	rows := buildRows(buildRowsParams{items: worktreeFixture(), order: worktreesOrder(tabs.State{})})

	assert.Len(t, rows, 8)
	assert.NotContains(t, xslice.Map(rows, func(r row) string { return r.name() }), "review-607")
}

// TestWorktreeRowNames covers the name falling from the task to the branch to the directory.
func TestWorktreeRowNames(t *testing.T) {
	rows := buildRows(buildRowsParams{items: worktreeFixture(), order: worktreesOrder(tabs.State{})})

	names := xslice.Map(rows, func(r row) string {
		return r.name()
	})

	assert.Equal(t, []string{
		"/home/h/gadgets/admin",
		"custom-cargo-fixes",
		"/home/h/komrad",
		"review-column",
		"worktree-view",
		"agent/orphan",
		"/var/folders/T/repo",
		"broken",
	}, names)
}

// TestWorktreeRowsHeadTheDirectoriesWithNoRepository covers a directory whose .git file names
// nothing sitting under a heading of its own.
func TestWorktreeRowsHeadTheDirectoriesWithNoRepository(t *testing.T) {
	rows := buildRows(buildRowsParams{items: []panel.Worktree{{Dir: "/trees/stray"}}, order: worktreesOrder(tabs.State{})})

	require.Len(t, rows, 2)
	assert.Equal(t, noRepository, rows[0].name())
	assert.Equal(t, "stray", rows[1].name())
}

// TestRowIndexOfWorktreeAgent covers the lead and another occupant both landing on the directory,
// and an agent with no directory landing nowhere.
func TestRowIndexOfWorktreeAgent(t *testing.T) {
	rows := buildRows(buildRowsParams{items: worktreeFixture(), order: worktreesOrder(tabs.State{})})

	tests := []struct {
		name string
		id   string
		want int
	}{
		{
			name: "no id lands nowhere",
			id:   "",
			want: -1,
		},
		{
			name: "an agent with no directory lands nowhere",
			id:   "elsewhere",
			want: -1,
		},
		{
			name: "the lead lands on its directory",
			id:   "live",
			want: 4,
		},
		{
			name: "an occupant lands on the directory it works in",
			id:   "occ",
			want: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, rowIndexOfAgent(rows, tt.id))
		})
	}
}

// TestWorktreesSnapshotReadsEveryRow covers the kind, the process, the status and the repo
// glyph each kind of row carries.
func TestWorktreesSnapshotReadsEveryRow(t *testing.T) {
	rows := buildRows(buildRowsParams{items: worktreeFixture(), order: worktreesOrder(tabs.State{})})
	snapshot := listSnapshot(listSnapshotParams{
		rows:     rows,
		selected: list.Selection{Keys: []string{"tree:/trees/orphan"}},
		focused:  "tree:/trees/live",
		live: []panel.AgentRow{{
			Agent: panel.Agent{
				ID:    "live",
				Name:  "worktree-view",
				State: panel.StateWaiting,
			},
			Alive:      true,
			UnreadMail: true,
		}},
	})

	byKey := map[string]view.Row{}
	for _, row := range snapshot.Rows {
		byKey[row.Key] = row
	}

	komrad := byKey["repo:/home/h/komrad"]
	assert.Equal(t, view.KindWorkflow, komrad.Kind)
	assert.Equal(t, view.ProcessNone, komrad.Process)

	live := byKey["tree:/trees/live"]
	assert.Equal(t, view.KindAgent, live.Kind)
	assert.Equal(t, view.ProcessAlive, live.Process)
	assert.Equal(t, view.StatusWaiting, live.Status, "the agents listing's state wins over the worktree listing's")
	assert.True(t, live.UnreadMail, "the marks come off the agents listing")
	assert.NotContains(t, byKey, "agent:occ", "an occupant is no row of its own")

	archived := byKey["tree:/trees/archived"]
	assert.Equal(t, view.ProcessStopped, archived.Process)
	assert.Equal(t, view.StatusIdle, archived.Status, "a stored thinking on an agent with no pane reads as idle")
	assert.True(t, archived.Archived)

	orphan := byKey["tree:/trees/orphan"]
	assert.Equal(t, view.ProcessStopped, orphan.Process)
	assert.True(t, orphan.Selected)

	assert.Equal(t, columns(), snapshot.Columns)
	assert.Equal(t, "tree:/trees/live", snapshot.Focused)
}

// TestWorktreeAgentFactsOffTheWorktreeListing covers an agent the agents listing does not hold
// reading the same as it would on the archive.
func TestWorktreeAgentFactsOffTheWorktreeListing(t *testing.T) {
	archived := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		state       panel.State
		archivedAt  *time.Time
		wantStatus  view.Status
		wantProcess view.Process
	}{
		{
			name:        "archived mid-turn",
			state:       panel.StateThinking,
			archivedAt:  &archived,
			wantStatus:  view.StatusIdle,
			wantProcess: view.ProcessStopped,
		},
		{
			name:        "archived while waiting",
			state:       panel.StateWaiting,
			archivedAt:  &archived,
			wantStatus:  view.StatusIdle,
			wantProcess: view.ProcessStopped,
		},
		{
			name:        "archived while unreadable",
			state:       panel.StateUncertain,
			archivedAt:  &archived,
			wantStatus:  view.StatusIdle,
			wantProcess: view.ProcessStopped,
		},
		{
			name:        "archived idle",
			state:       panel.StateIdle,
			archivedAt:  &archived,
			wantStatus:  view.StatusIdle,
			wantProcess: view.ProcessStopped,
		},
		{
			name:        "left behind by a failed cleanup",
			state:       panel.StateFailedToCleanup,
			archivedAt:  &archived,
			wantStatus:  view.StatusBroken,
			wantProcess: view.ProcessAlive,
		},
		{
			name:        "not yet in the agents listing",
			state:       panel.StateThinking,
			wantStatus:  view.StatusIdle,
			wantProcess: view.ProcessStopped,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := agentFacts(view.Row{}, panel.WorktreeAgent{
				ID:         "gone",
				State:      tt.state,
				ArchivedAt: tt.archivedAt,
			}, map[string]panel.AgentRow{})

			assert.Equal(t, tt.wantStatus, row.Status)
			assert.Equal(t, tt.wantProcess, row.Process)
			assert.Equal(t, tt.archivedAt != nil, row.Archived)
		})
	}
}

// TestWorktreesSnapshotCellsAreSizeAgeThenStanding covers every row's cells: the size and the age
// on a directory with a dash for the size until it is measured or while its walk fails, the
// standing in the glyphs an agent's row renders, and the header summing the shares of the measured
// directories under it over the size column.
func TestWorktreesSnapshotCellsAreSizeAgeThenStanding(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	items := measuredFixture(now)
	snapshot := listSnapshot(listSnapshotParams{
		rows:  buildRows(buildRowsParams{items: items, order: worktreesOrder(tabs.State{})}),
		items: items,
		now:   now,
	})

	cells := map[string][]view.Cell{}
	for _, row := range snapshot.Rows {
		cells[row.Key] = row.Cells
	}

	assert.Equal(t, map[string][]view.Cell{
		"repo:/home/h/gadgets/admin": {{Text: "1.5G", Tone: paint.ToneMuted}},
		"tree:/trees/admin":          {{Text: "1.5G", Tone: paint.ToneMuted}, {Text: "12h", Tone: paint.ToneMuted}, {Glyph: glyph.Merged, Tone: paint.ToneMerged}},
		"repo:/home/h/komrad":        {{Text: "60M", Tone: paint.ToneMuted}},
		"tree:/trees/live":           {{Text: "49M", Tone: paint.ToneMuted}, {Text: "30s", Tone: paint.ToneMuted}, {Glyph: glyph.Worktree, Tone: paint.ToneWarning}},
		"tree:/trees/archived":       {{Text: "49M", Tone: paint.ToneMuted}, {Text: "4d", Tone: paint.ToneMuted}, {Glyph: glyph.Worktree, Tone: paint.ToneMuted}},
		"tree:/trees/orphan":         {{Text: "—", Tone: paint.ToneMuted}, {Text: "3h", Tone: paint.ToneMuted}, {Text: "—", Tone: paint.ToneMuted}},
		"repo:/var/folders/T/repo":   {{Text: "—", Tone: paint.ToneMuted}},
		"tree:/trees/broken":         {{Text: "—", Tone: paint.ToneMuted}, {Text: "2w", Tone: paint.ToneMuted}, {Glyph: glyph.WorktreeGone, Tone: paint.ToneDanger}},
	}, cells)
}

// worktreeFixture is two repositories' worth of directories: a live task with an agent and a
// reviewer in it, one archived with an agent stopped mid-turn still stored, a directory nothing
// answers for, an unreadable one, and another archived task's.
func worktreeFixture() []panel.Worktree {
	archived := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

	return []panel.Worktree{
		{
			Dir:      "/trees/live",
			RepoDir:  "/home/h/komrad",
			Branch:   "agent/live",
			Readable: true,
			Task: &panel.Task{
				ID:   "live",
				Slug: "worktree-view",
			},
			Occupants: []panel.WorktreeAgent{
				{
					ID:    "live",
					Name:  "worktree-view",
					State: panel.StateThinking,
				},
				{
					ID:    "occ",
					Name:  "review-607",
					State: panel.StateIdle,
				},
			},
			Fate: panel.FateLive,
		},
		{
			Dir:      "/trees/archived",
			RepoDir:  "/home/h/komrad",
			Branch:   "agent/archived",
			Readable: true,
			Task: &panel.Task{
				ID:         "archived",
				Slug:       "review-column",
				ArchivedAt: &archived,
			},
			Occupants: []panel.WorktreeAgent{{
				ID:         "archived",
				Name:       "review-column",
				State:      panel.StateThinking,
				ArchivedAt: &archived,
			}},
			Fate: panel.FateArchived,
		},
		{
			Dir:      "/trees/orphan",
			RepoDir:  "/home/h/komrad",
			Branch:   "agent/orphan",
			Readable: true,
		},
		{
			Dir:     "/trees/broken",
			RepoDir: "/var/folders/T/repo",
		},
		{
			Dir:      "/trees/admin",
			RepoDir:  "/home/h/gadgets/admin",
			Branch:   "agent/admin",
			Readable: true,
			Task: &panel.Task{
				ID:         "admin",
				Slug:       "custom-cargo-fixes",
				ArchivedAt: &archived,
			},
			Occupants: []panel.WorktreeAgent{{
				ID:         "admin",
				Name:       "custom-cargo-fixes",
				State:      panel.StateIdle,
				ArchivedAt: &archived,
			}},
			Fate: panel.FateArchived,
		},
	}
}

// worktreesOrder is the order the strip names for a state.
func worktreesOrder(state tabs.State) tabs.Order {
	return sortStrip().Order(state)
}

// measuredFixture is the fixture with the loop having been round: the live and archived
// directories under komrad measured, the orphan's walk failed, the admin one measured under its
// own repository, and each with a last activity behind the given moment.
func measuredFixture(now time.Time) []panel.Worktree {
	items := worktreeFixture()
	items[0].Size = &panel.WorktreeSize{
		Bytes:      49 * 1024 * 1024,
		Share:      30 * 1024 * 1024,
		MeasuredAt: now.Add(-time.Minute),
	}
	items[0].LastActiveAt = now.Add(-30 * time.Second)
	items[1].Size = &panel.WorktreeSize{
		Bytes:      49 * 1024 * 1024,
		Share:      30 * 1024 * 1024,
		MeasuredAt: now.Add(-time.Hour),
	}
	items[1].LastActiveAt = now.Add(-4 * 24 * time.Hour)
	items[2].Size = &panel.WorktreeSize{MeasuredAt: now.Add(-time.Hour), Err: "permission denied"}
	items[2].LastActiveAt = now.Add(-3 * time.Hour)
	items[3].LastActiveAt = now.Add(-2 * 7 * 24 * time.Hour)
	items[4].Size = &panel.WorktreeSize{
		Bytes:      1536 * 1024 * 1024,
		Share:      1536 * 1024 * 1024,
		MeasuredAt: now.Add(-time.Hour),
	}
	items[4].LastActiveAt = now.Add(-12 * time.Hour)
	items[0].Standing = &panel.WorktreeStanding{Dirty: true}
	items[1].Standing = &panel.WorktreeStanding{Unlanded: 3}
	items[2].Standing = &panel.WorktreeStanding{Err: "git cannot read the directory"}
	items[4].Standing = &panel.WorktreeStanding{}

	return items
}
