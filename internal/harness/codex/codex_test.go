package codex

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMintSessionID pins the difference that shapes this harness. Codex takes no
// session id on the command line, so the panel cannot name a conversation before it
// exists and has to adopt the one Codex chose.
func TestMintSessionID(t *testing.T) {
	assert.Empty(t, New(Options{SessionsDir: t.TempDir()}).MintSessionID())
}

// TestFindSessions covers adopting that conversation. A rollout belongs to an agent
// when it was started in that agent's directory after the agent was created, and the
// answer is ordered oldest first so a caller pairing them off gets the earliest
// conversation with the earliest agent.
func TestFindSessions(t *testing.T) {
	launched := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)

	// rollout describes one file on disk: when it was written, the directory its
	// session_meta claims, and whether that opening record is there at all.
	type rollout struct {
		id       string
		stamp    string
		cwd      string
		modTime  time.Time
		noMeta   bool
		unparsed bool
	}

	tests := []struct {
		name     string
		dir      string
		rollouts []rollout
		want     []string
	}{
		{
			name: "no rollouts at all, which is every pass before the CLI has written one",
			dir:  "/repo/worktree",
		},
		{
			name: "a rollout from another directory is another agent's",
			dir:  "/repo/worktree",
			rollouts: []rollout{
				{id: "sess-elsewhere", stamp: "12-05-00", cwd: "/repo/other", modTime: launched.Add(time.Minute)},
			},
		},
		{
			name: "a rollout older than the agent belongs to whatever ran there before it",
			dir:  "/repo/worktree",
			rollouts: []rollout{
				{id: "sess-earlier", stamp: "11-00-00", cwd: "/repo/worktree", modTime: launched.Add(-time.Hour)},
			},
		},
		{
			name: "a rollout caught before its opening record is complete is not yet claimable",
			dir:  "/repo/worktree",
			rollouts: []rollout{
				{id: "sess-partial", stamp: "12-05-00", cwd: "/repo/worktree", modTime: launched.Add(time.Minute), unparsed: true},
			},
		},
		{
			name: "a file that opens with something other than a session_meta is not a conversation",
			dir:  "/repo/worktree",
			rollouts: []rollout{
				{id: "sess-headless", stamp: "12-05-00", cwd: "/repo/worktree", modTime: launched.Add(time.Minute), noMeta: true},
			},
		},
		{
			name: "the one started here after the agent was created",
			dir:  "/repo/worktree",
			rollouts: []rollout{
				{id: "sess-mine", stamp: "12-05-00", cwd: "/repo/worktree", modTime: launched.Add(time.Minute)},
			},
			want: []string{"sess-mine"},
		},
		{
			name: "two started here come back oldest first",
			dir:  "/repo/worktree",
			rollouts: []rollout{
				{id: "sess-second", stamp: "12-06-00", cwd: "/repo/worktree", modTime: launched.Add(2 * time.Minute)},
				{id: "sess-first", stamp: "12-05-00", cwd: "/repo/worktree", modTime: launched.Add(time.Minute)},
			},
			want: []string{"sess-first", "sess-second"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessions := t.TempDir()

			for _, r := range tt.rollouts {
				body := `{"type":"session_meta","payload":{"id":"` + r.id + `","cwd":"` + r.cwd + `"}}` + "\n"
				switch {
				case r.unparsed:
					body = `{"type":"session_meta","payload":{"id":"` + r.id
				case r.noMeta:
					body = `{"type":"event_msg","payload":{"type":"task_started"}}` + "\n"
				}

				path := writeRollout(t, sessions, "2026-07-30T"+r.stamp, r.id, body)
				require.NoError(t, os.Chtimes(path, r.modTime, r.modTime))
			}

			found, err := New(Options{SessionsDir: sessions}).FindSessions(tt.dir, launched)
			require.NoError(t, err)
			assert.Equal(t, tt.want, found)
		})
	}
}

// TestRelocate pins that a restore has nothing to move. Codex resumes by id from
// anywhere, so an agent brought back in a stand-in directory keeps its conversation
// without the panel carrying anything across.
func TestRelocate(t *testing.T) {
	assert.NoError(t, New(Options{SessionsDir: t.TempDir()}).Relocate("sess-1", "/somewhere/else"))
}
