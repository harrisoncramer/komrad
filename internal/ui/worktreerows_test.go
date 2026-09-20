package ui

import (
	"testing"
	"time"

	agentstore "weave/internal/store/agent"
	"weave/internal/view"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// worktreeFixture is two repositories' worth of directories: a live agent, an archived one, a
// directory nothing answers for, an unreadable one, and an occupant working in the live one.
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
				state:      agentstore.StateIdle,
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

// TestWorktreeRowsGroupByRepositoryAndStanding covers the headers in path order, the live row
// first under its repository, then the archived, then the one nothing answers for, with no row
// for the occupant of the live one.
func TestWorktreeRowsGroupByRepositoryAndStanding(t *testing.T) {
	rows := worktreeRows(worktreeFixture())

	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, row.key())
	}

	assert.Equal(t, []string{
		"repo:/home/h/chariot/admin",
		"tree:/trees/admin",
		"repo:/home/h/weave",
		"tree:/trees/live",
		"tree:/trees/archived",
		"tree:/trees/orphan",
		"repo:/var/folders/T/repo",
		"tree:/trees/broken",
	}, keys)
}

// TestWorktreeRowNames covers the name falling from the agent to the branch to the directory.
func TestWorktreeRowNames(t *testing.T) {
	rows := worktreeRows(worktreeFixture())

	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.name())
	}

	assert.Equal(t, []string{
		"/home/h/chariot/admin",
		"custom-cargo-fixes",
		"/home/h/weave",
		"worktree-view",
		"review-column",
		"agent/orphan",
		"/var/folders/T/repo",
		"broken",
	}, names)
}

// TestWorktreeRowsHeadTheDirectoriesWithNoRepository covers a directory whose .git file names
// nothing sitting under a heading of its own.
func TestWorktreeRowsHeadTheDirectoriesWithNoRepository(t *testing.T) {
	rows := worktreeRows([]worktreeItem{{dir: "/trees/stray"}})

	require.Len(t, rows, 2)
	assert.Equal(t, noRepository, rows[0].name())
	assert.Equal(t, "stray", rows[1].name())
}

// TestRowIndexOfWorktreeAgent covers the owner and an occupant both landing on the directory,
// and an agent with no directory landing nowhere.
func TestRowIndexOfWorktreeAgent(t *testing.T) {
	rows := worktreeRows(worktreeFixture())

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
			want: 3,
		},
		{
			name: "an occupant lands on the directory it works in",
			id:   "occ",
			want: 3,
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
	rows := worktreeRows(worktreeFixture())
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
	assert.True(t, archived.Archived)

	orphan := byKey["tree:/trees/orphan"]
	assert.Equal(t, view.ProcessStopped, orphan.Process)
	assert.True(t, orphan.Selected)

	broken := byKey["tree:/trees/broken"]
	assert.Equal(t, view.RepoGone, broken.Repo)

	assert.Equal(t, worktreesDropped, snapshot.Dropped)
	assert.Equal(t, "tree:/trees/live", snapshot.Focused)
}

// TestWorktreeStatusReadsTheListingsState covers the mapping a row falls back on while the
// agents listing does not hold its agent.
func TestWorktreeStatusReadsTheListingsState(t *testing.T) {
	tests := []struct {
		state agentstore.State
		want  view.Status
	}{
		{state: agentstore.StateThinking, want: view.StatusThinking},
		{state: agentstore.StateSettingUp, want: view.StatusThinking},
		{state: agentstore.StateWaiting, want: view.StatusWaiting},
		{state: agentstore.StateUncertain, want: view.StatusWaiting},
		{state: agentstore.StateFailedToCleanup, want: view.StatusBroken},
		{state: agentstore.StateIdle, want: view.StatusIdle},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			assert.Equal(t, tt.want, worktreeStatus(tt.state))
		})
	}
}

// TestBytesLabel covers the unit and the precision at each size, and the roll into the next
// unit where the rounded value would run to five characters.
func TestBytesLabel(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{bytes: 0, want: "0B"},
		{bytes: 512, want: "512B"},
		{bytes: 999, want: "999B"},
		{bytes: 1000, want: "1.0K"},
		{bytes: 1023, want: "1.0K"},
		{bytes: 10240, want: "10K"},
		{bytes: 848 * 1024, want: "848K"},
		{bytes: 9*1024*1024 + 200*1024, want: "9.2M"},
		{bytes: 1048063, want: "1.0M"},
		{bytes: 49 * 1024 * 1024, want: "49M"},
		{bytes: 1536 * 1024 * 1024, want: "1.5G"},
		{bytes: 24 * 1024 * 1024 * 1024, want: "24G"},
		{bytes: 1024*1024*1024*1024 - 1, want: "1.0T"},
		{bytes: 3 * 1024 * 1024 * 1024 * 1024, want: "3.0T"},
		{bytes: 2048 * 1024 * 1024 * 1024 * 1024, want: "2048T"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			label := bytesLabel(tt.bytes)

			assert.Equal(t, tt.want, label)
			if tt.bytes < 1000*1024*1024*1024*1024 {
				assert.LessOrEqual(t, len(label), 4)
			}
		})
	}
}
