package ui

import (
	"testing"
	"time"

	agentstore "weave/internal/store/agent"
	"weave/internal/view"
	"weave/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// worktreeFixture is two repositories' worth of directories: a live agent, one archived mid-turn
// with the state it stopped in still stored, a directory nothing answers for, an unreadable one,
// and an occupant working in the live one.
func worktreeFixture() []worktreeItem {
	archived := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

	return []worktreeItem{
		{
			dir:      "/trees/live",
			repoDir:  "/home/h/weave",
			branch:   "agent/live",
			readable: true,
			agent: &worktreeAgent{
				id:    "live",
				name:  "worktree-view",
				state: agentstore.StateThinking,
			},
			occupants: []worktreeAgent{{
				id:    "occ",
				name:  "review-607",
				state: agentstore.StateIdle,
			}},
		},
		{
			dir:      "/trees/archived",
			repoDir:  "/home/h/weave",
			branch:   "agent/archived",
			readable: true,
			agent: &worktreeAgent{
				id:         "archived",
				name:       "review-column",
				state:      agentstore.StateThinking,
				archivedAt: &archived,
			},
		},
		{
			dir:      "/trees/orphan",
			repoDir:  "/home/h/weave",
			branch:   "agent/orphan",
			readable: true,
		},
		{
			dir:     "/trees/broken",
			repoDir: "/var/folders/T/repo",
		},
		{
			dir:      "/trees/admin",
			repoDir:  "/home/h/chariot/admin",
			branch:   "agent/admin",
			readable: true,
			agent: &worktreeAgent{
				id:         "admin",
				name:       "custom-cargo-fixes",
				state:      agentstore.StateIdle,
				archivedAt: &archived,
			},
		},
	}
}

// worktreesOrder is the order the strip names for a state.
func worktreesOrder(state tabsState) sortOrder {
	return worktreesTabs().order(state)
}

// TestWorktreeRowsOrderEachGroupByTheColumn covers the headers staying in path order under
// every sort, the rows under each ordered by the chosen column in the arrow's direction with
// the rows that lack the value at the bottom, and the directory name settling a tie.
func TestWorktreeRowsOrderEachGroupByTheColumn(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	items := measuredFixture(now)
	items[0].size.bytes = 60 * 1024 * 1024

	tests := []struct {
		name  string
		items []worktreeItem
		state tabsState
		want  []string
	}{
		{
			name:  "nothing measured falls to the directory name",
			items: worktreeFixture(),
			state: tabsState{chosen: "size", direction: tabUp},
			want: []string{
				"repo:/home/h/chariot/admin",
				"tree:/trees/admin",
				"repo:/home/h/weave",
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
			state: tabsState{},
			want: []string{
				"repo:/home/h/chariot/admin",
				"tree:/trees/admin",
				"repo:/home/h/weave",
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
			state: tabsState{chosen: "size", direction: tabUp},
			want: []string{
				"repo:/home/h/chariot/admin",
				"tree:/trees/admin",
				"repo:/home/h/weave",
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
			state: tabsState{chosen: "last-accessed", direction: tabDown},
			want: []string{
				"repo:/home/h/chariot/admin",
				"tree:/trees/admin",
				"repo:/home/h/weave",
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
			state: tabsState{chosen: "last-accessed", direction: tabUp},
			want: []string{
				"repo:/home/h/chariot/admin",
				"tree:/trees/admin",
				"repo:/home/h/weave",
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
			rows := worktreeRows(tt.items, worktreesOrder(tt.state))

			assert.Equal(t, tt.want, xslice.Map(rows, worktreeRow.key))
		})
	}
}

// TestWorktreeRowsLeaveTheOccupantOffTheList covers an agent working in another's directory
// getting no row of its own.
func TestWorktreeRowsLeaveTheOccupantOffTheList(t *testing.T) {
	rows := worktreeRows(worktreeFixture(), worktreesOrder(tabsState{}))

	assert.Len(t, rows, 8)
	for _, row := range rows {
		if row.item.agent != nil {
			assert.NotEqual(t, "occ", row.item.agent.id)
		}
	}
}

// TestWorktreeRowNames covers the name falling from the agent to the branch to the directory.
func TestWorktreeRowNames(t *testing.T) {
	rows := worktreeRows(worktreeFixture(), worktreesOrder(tabsState{}))

	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.name())
	}

	assert.Equal(t, []string{
		"/home/h/chariot/admin",
		"custom-cargo-fixes",
		"/home/h/weave",
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
	rows := worktreeRows([]worktreeItem{{dir: "/trees/stray"}}, worktreesOrder(tabsState{}))

	require.Len(t, rows, 2)
	assert.Equal(t, noRepository, rows[0].name())
	assert.Equal(t, "stray", rows[1].name())
}

// TestRowIndexOfWorktreeAgent covers the owner and an occupant both landing on the directory,
// and an agent with no directory landing nowhere.
func TestRowIndexOfWorktreeAgent(t *testing.T) {
	rows := worktreeRows(worktreeFixture(), worktreesOrder(tabsState{}))

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
			name: "the owner lands on its directory",
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
			assert.Equal(t, tt.want, rowIndexOfWorktreeAgent(rows, tt.id))
		})
	}
}

// TestWorktreesSnapshotReadsEveryRow covers the kind, the process, the status and the repo
// glyph each kind of row carries.
func TestWorktreesSnapshotReadsEveryRow(t *testing.T) {
	rows := worktreeRows(worktreeFixture(), worktreesOrder(tabsState{}))
	snapshot := worktreesSnapshot(worktreesSnapshotParams{
		rows:     rows,
		selected: selection{keys: []string{"tree:/trees/orphan"}},
		focused:  "tree:/trees/live",
		live: []agentItem{{
			agent: agentstore.Agent{
				ID:    "live",
				Name:  "worktree-view",
				State: agentstore.StateWaiting,
			},
			alive:      true,
			unreadMail: true,
		}},
	})

	byKey := map[string]view.Row{}
	for _, row := range snapshot.Rows {
		byKey[row.Key] = row
	}

	weave := byKey["repo:/home/h/weave"]
	assert.Equal(t, view.KindWorkflow, weave.Kind)
	assert.Equal(t, view.ProcessNone, weave.Process)

	live := byKey["tree:/trees/live"]
	assert.Equal(t, view.KindAgent, live.Kind)
	assert.Equal(t, view.ProcessAlive, live.Process)
	assert.Equal(t, view.StatusWaiting, live.Status, "the agents listing's state wins over the worktree listing's")
	assert.True(t, live.UnreadMail, "the marks come off the agents listing")
	assert.Equal(t, view.RepoClean, live.Repo)
	assert.NotContains(t, byKey, "agent:occ", "an occupant is no row of its own")

	archived := byKey["tree:/trees/archived"]
	assert.Equal(t, view.ProcessStopped, archived.Process)
	assert.Equal(t, view.StatusIdle, archived.Status, "a stored thinking on an agent with no pane reads as idle")
	assert.True(t, archived.Archived)

	orphan := byKey["tree:/trees/orphan"]
	assert.Equal(t, view.ProcessStopped, orphan.Process)
	assert.True(t, orphan.Selected)

	broken := byKey["tree:/trees/broken"]
	assert.Equal(t, view.RepoGone, broken.Repo)

	assert.Equal(t, worktreesDropped, snapshot.Dropped)
	assert.Equal(t, "tree:/trees/live", snapshot.Focused)
}

// TestWorktreeAgentFactsOffTheWorktreeListing covers an agent the agents listing does not hold
// reading the same as it would on the archive.
func TestWorktreeAgentFactsOffTheWorktreeListing(t *testing.T) {
	archived := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		state       agentstore.State
		archivedAt  *time.Time
		wantStatus  view.Status
		wantProcess view.Process
	}{
		{
			name:        "archived mid-turn",
			state:       agentstore.StateThinking,
			archivedAt:  &archived,
			wantStatus:  view.StatusIdle,
			wantProcess: view.ProcessStopped,
		},
		{
			name:        "archived while waiting",
			state:       agentstore.StateWaiting,
			archivedAt:  &archived,
			wantStatus:  view.StatusIdle,
			wantProcess: view.ProcessStopped,
		},
		{
			name:        "archived while unreadable",
			state:       agentstore.StateUncertain,
			archivedAt:  &archived,
			wantStatus:  view.StatusIdle,
			wantProcess: view.ProcessStopped,
		},
		{
			name:        "archived idle",
			state:       agentstore.StateIdle,
			archivedAt:  &archived,
			wantStatus:  view.StatusIdle,
			wantProcess: view.ProcessStopped,
		},
		{
			name:        "left behind by a failed cleanup",
			state:       agentstore.StateFailedToCleanup,
			archivedAt:  &archived,
			wantStatus:  view.StatusBroken,
			wantProcess: view.ProcessAlive,
		},
		{
			name:        "not yet in the agents listing",
			state:       agentstore.StateThinking,
			wantStatus:  view.StatusIdle,
			wantProcess: view.ProcessStopped,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := worktreeAgentFacts(view.Row{}, worktreeAgent{
				id:         "gone",
				state:      tt.state,
				archivedAt: tt.archivedAt,
			}, map[string]agentItem{})

			assert.Equal(t, tt.wantStatus, row.Status)
			assert.Equal(t, tt.wantProcess, row.Process)
			assert.Equal(t, tt.archivedAt != nil, row.Archived)
		})
	}
}

// measuredFixture is the fixture with the loop having been round: the live and archived
// directories under weave measured, the orphan's walk failed, the admin one measured under its
// own repository, and each with a last activity behind the given moment.
func measuredFixture(now time.Time) []worktreeItem {
	items := worktreeFixture()
	items[0].size = &worktreeSize{bytes: 49 * 1024 * 1024, measuredAt: now.Add(-time.Minute)}
	items[0].lastActiveAt = now.Add(-30 * time.Second)
	items[1].size = &worktreeSize{bytes: 49 * 1024 * 1024, measuredAt: now.Add(-time.Hour)}
	items[1].lastActiveAt = now.Add(-4 * 24 * time.Hour)
	items[2].size = &worktreeSize{measuredAt: now.Add(-time.Hour), err: "permission denied"}
	items[2].lastActiveAt = now.Add(-3 * time.Hour)
	items[3].lastActiveAt = now.Add(-2 * 7 * 24 * time.Hour)
	items[4].size = &worktreeSize{bytes: 1536 * 1024 * 1024, measuredAt: now.Add(-time.Hour)}
	items[4].lastActiveAt = now.Add(-12 * time.Hour)

	return items
}

// TestWorktreesSnapshotTrailsSizeThenAge covers every row's trail: the size then the age on a
// directory, a dash for the size until it is measured or while its walk fails, and the header
// summing the measured directories under it over the size column.
func TestWorktreesSnapshotTrailsSizeThenAge(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	items := measuredFixture(now)
	snapshot := worktreesSnapshot(worktreesSnapshotParams{
		rows:  worktreeRows(items, worktreesOrder(tabsState{})),
		items: items,
		now:   now,
	})

	trails := map[string]string{}
	for _, row := range snapshot.Rows {
		trails[row.Key] = row.Trail
	}

	assert.Equal(t, map[string]string{
		"repo:/home/h/chariot/admin": "1.5G    ",
		"tree:/trees/admin":          "1.5G 12h",
		"repo:/home/h/weave":         " 98M    ",
		"tree:/trees/live":           " 49M 30s",
		"tree:/trees/archived":       " 49M  4d",
		"tree:/trees/orphan":         "   —  3h",
		"repo:/var/folders/T/repo":   "   —    ",
		"tree:/trees/broken":         "   —  2w",
	}, trails)
}
