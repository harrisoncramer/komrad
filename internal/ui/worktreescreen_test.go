package ui_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"
	worktreev1 "weave/proto/gen/worktree/v1"
	"weave/proto/gen/worktree/v1/worktreev1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

const worktreesTitle = "Worktrees"

// TestWorktreesOpen covers the screen opening on the directory of the agent the cursor was on.
func TestWorktreesOpen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		press       []string
		wantHovered string
	}{
		{
			name:        "from the agents list it lands on the hovered agent's directory",
			press:       []string{"t"},
			wantHovered: "treeview",
		},
		{
			name:        "an occupant lands on the directory it works in",
			press:       []string{"j", "t"},
			wantHovered: "treeview",
		},
		{
			name:        "from the archive it lands on the archived agent's directory",
			press:       []string{"Z", "j", "t"},
			wantHovered: "reviewer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openAgents(t, buildWorktrees()...)
			answerWorktreeReads(harness)

			harness.press(tt.press...)

			frame := harness.readFrame()
			assert.Contains(t, frame, worktreesTitle)
			assert.Equal(t, tt.wantHovered, readHoveredWorktree(frame))
		})
	}

	t.Run("the panel keeps the whole window, a live agent's pane included", func(t *testing.T) {
		t.Parallel()

		rows := buildWorktrees()
		rows[1].(*agentv1.Agent).PaneId = "%7"
		harness := openAgents(t, rows...)
		harness.press("t")

		assert.True(t, harness.panel.IsZoomed())
	})

	t.Run("the title counts the directories and totals what has been measured", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		require.Contains(t, splitLines(harness.readFrame())[0], "5 · —")

		harness.watch(buildPuts(
			newMeasuredWorktree("/trees/live", 49*1024*1024),
			newMeasuredWorktree("/trees/archived", 49*1024*1024),
			newMeasuredWorktree("/trees/admin", 1536*1024*1024),
		))

		assert.Contains(t, splitLines(harness.readFrame())[0], "5 · 1.6G")
	})
}

// TestWorktreesJump covers jumping from a directory to the agent it belongs to.
func TestWorktreesJump(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		row         string
		wantTitle   string
		wantHovered string
	}{
		{
			name:      "a directory nothing answers for stays put",
			row:       "agent/orphan",
			wantTitle: worktreesTitle,
		},
		{
			name:        "a live agent's directory lands on the agents list",
			row:         "treeview",
			wantTitle:   agentsTitle,
			wantHovered: "treeview",
		},
		{
			name:        "an archived agent's directory lands in the archive",
			row:         "reviewer",
			wantTitle:   archiveTitle,
			wantHovered: "reviewer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openWorktrees(t)
			moveToWorktree(t, harness, tt.row)

			harness.press("J")

			frame := harness.readFrame()
			assert.Contains(t, frame, tt.wantTitle)
			assert.Contains(t, readHoveredRow(frame), tt.wantHovered)
		})
	}
}

// TestWorktreesDelete covers what each delete key opens on the row under the cursor.
func TestWorktreesDelete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		row        string
		press      []string
		wantDrawn  string
		wantAbsent string
	}{
		{
			name:       "a repository header opens nothing",
			row:        "/home/h/weave",
			press:      []string{"d"},
			wantAbsent: "Delete",
		},
		{
			name:      "a selection holding a live agent is refused",
			row:       "treeview",
			press:     []string{"v", "j", "v", "d"},
			wantDrawn: "live agent",
		},
		{
			name:      "a live agent is offered the archive",
			row:       "treeview",
			press:     []string{"d"},
			wantDrawn: `Archive agent "treeview"?`,
		},
		{
			name:      "an archived agent's directory is offered the delete, which keeps a branch nothing else holds",
			row:       "reviewer",
			press:     []string{"d"},
			wantDrawn: "Delete the worktree of reviewer? The directory goes through the agent delete",
		},
		{
			name:      "a directory nothing answers for is offered the delete on its own",
			row:       "broken",
			press:     []string{"d"},
			wantDrawn: "Delete the worktree of broken? The directory nothing answers for is removed on its own",
		},
		{
			name:      "a selection of an archived and two unowned directories counts them",
			row:       "reviewer",
			press:     []string{"v", "j", "j", "v", "j", "j", "v", "d"},
			wantDrawn: "Delete 3 worktrees? The directory goes through the agent delete",
		},
		{
			name:      "clear gathers everything but the live directory",
			row:       "treeview",
			press:     []string{"D"},
			wantDrawn: "Delete 4 worktrees?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openWorktrees(t)
			moveToWorktree(t, harness, tt.row)

			harness.press(tt.press...)

			frame := harness.readFrame()
			prose := readFormProse(frame) + " " + readSidebarProse(frame)
			assert.Contains(t, prose, tt.wantDrawn)
			if tt.wantAbsent != "" {
				assert.NotContains(t, prose, tt.wantAbsent)
			}
		})
	}
}

// TestWorktreesDeleteBranch covers the delete prompt saying what happens to a single directory's branch.
func TestWorktreesDeleteBranch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		keepsBranch bool
		want        string
	}{
		{
			name: "a branch whose commits are all elsewhere goes too",
			want: "The branch goes too",
		},
		{
			name:        "a branch holding the only copy of its commits is kept",
			keepsBranch: true,
			want:        "The branch is kept",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openWorktrees(t)
			harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(&worktreev1.GetWorktreeResponse{
				Worktree:    &worktreev1.Worktree{Dir: "/trees/archived"},
				BaseRef:     "origin/main",
				KeepsBranch: tt.keepsBranch,
			}))
			moveToWorktree(t, harness, "reviewer")
			harness.fireTimers()

			harness.press("d")

			assert.Contains(t, readFormProse(harness.readFrame()), tt.want)
		})
	}
}

// TestWorktreesDeleteAnswer covers answering the prompt the clear key opens.
func TestWorktreesDeleteAnswer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		refusal  error
		answer   string
		wantSent []proto.Message
		want     string
	}{
		{
			name:     "a queue that refuses is reported",
			refusal:  errors.New("the queue is away"),
			answer:   "y",
			wantSent: []proto.Message{&worktreev1.DeleteWorktreesRequest{Dirs: []string{"/trees/admin", "/trees/archived", "/trees/orphan", "/trees/broken"}}},
			want:     "Could not delete 4 worktrees",
		},
		{
			name:   "declining queues nothing",
			answer: "n",
			want:   "No messages",
		},
		{
			name:     "confirming queues every directory that can go in one call",
			answer:   "y",
			wantSent: []proto.Message{&worktreev1.DeleteWorktreesRequest{Dirs: []string{"/trees/admin", "/trees/archived", "/trees/orphan", "/trees/broken"}}},
			want:     "Deleting 4 worktrees",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openWorktrees(t)
			if tt.refusal != nil {
				harness.refuse(worktreev1connect.WorktreeServiceDeleteWorktreesProcedure, tt.refusal)
			}
			harness.press(widenListPresses...)

			harness.press("D", tt.answer)

			harness.assertSentTo(worktreev1connect.WorktreeServiceDeleteWorktreesProcedure, tt.wantSent...)
			frame := harness.readFrame()
			assert.Contains(t, frame, tt.want)
			assert.NotContains(t, frame, "Delete 4 worktrees?")
		})
	}
}

// TestWorktreesRecord covers the record the list draws for a directory.
func TestWorktreesRecord(t *testing.T) {
	t.Parallel()

	t.Run("the listing names the agent through the list that holds it", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		moveToWorktree(t, harness, "reviewer")
		harness.press(widenListPresses...)

		frame := harness.readFrame()
		assert.Equal(t, "reviewer", readRecordField(frame, "name"))
		assert.Equal(t, "—", readRecordField(frame, "size"))
		assert.Equal(t, "agent/archived", readRecordField(frame, "branch"))
		assert.Equal(t, "weave", readRecordField(frame, "workflow"))
		assert.True(t, strings.HasPrefix(readRecordField(frame, "agent"), "claude, archived "))
		assert.Empty(t, readRecordField(frame, "base"))
	})

	t.Run("the read of the directory adds where the branch stands and what it changed", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(&worktreev1.GetWorktreeResponse{
			Worktree:  &worktreev1.Worktree{Dir: "/trees/archived"},
			BaseRef:   "origin/main",
			Ahead:     2,
			Behind:    12,
			Modified:  []string{"a.go"},
			Untracked: []string{"b.txt", "c.txt"},
		}))
		moveToWorktree(t, harness, "reviewer")
		harness.fireTimers()
		harness.press(widenListPresses...)

		frame := harness.readFrame()
		assert.Equal(t, "origin/main  ↑2 ↓12", readRecordField(frame, "base"))
		assert.Equal(t, "1 modified, 2 untracked", readRecordField(frame, "changes"))
	})

	t.Run("a directory git cannot read names its repository and no agent", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		moveToWorktree(t, harness, "broken")
		harness.press(widenListPresses...)

		frame := harness.readFrame()
		assert.Equal(t, "/var/folders/T/repo", readRecordField(frame, "repository"))
		assert.Equal(t, "none", readRecordField(frame, "agent"))
		assert.Empty(t, readRecordField(frame, "branch"))
	})
}

// TestWorktreesRecordActivity covers the record saying what a directory takes and when it was last active.
func TestWorktreesRecordActivity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		worktree       *worktreev1.WatchedWorktree
		row            string
		wantSize       string
		wantLastActive string
	}{
		{
			name:           "nothing measured and nothing touched",
			worktree:       &worktreev1.WatchedWorktree{Dir: "/trees/orphan", Readable: true, Branch: "agent/orphan"},
			row:            "agent/orphan",
			wantSize:       "—",
			wantLastActive: "—",
		},
		{
			name: "a failed walk says why",
			worktree: &worktreev1.WatchedWorktree{
				Dir:        "/trees/orphan",
				Readable:   true,
				Branch:     "agent/orphan",
				MeasuredAt: proto.String(stampAgo(5 * time.Minute)),
				SizeError:  "permission denied",
				ModifiedAt: stampAgo(90 * time.Minute),
			},
			row:            "agent/orphan",
			wantSize:       "—, could not measure: permission denied",
			wantLastActive: "1h ago",
		},
		{
			name: "a measured directory whose agent was put away",
			worktree: &worktreev1.WatchedWorktree{
				Dir:        "/trees/archived",
				Readable:   true,
				Branch:     "agent/archived",
				SizeBytes:  proto.Int64(49 * 1024 * 1024),
				MeasuredAt: proto.String(stampAgo(5 * time.Minute)),
			},
			row:            "reviewer",
			wantSize:       "49M, measured 5m ago",
			wantLastActive: "4d ago, archived",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rows := buildWorktrees()
			rows[3].(*agentv1.Agent).ArchivedAt = proto.String(stampAgo(4 * 24 * time.Hour))
			harness := openWorktrees(t, rows...)
			harness.watch(buildPuts(tt.worktree))
			moveToWorktree(t, harness, tt.row)
			harness.press(widenListPresses...)

			frame := harness.readFrame()
			assert.Equal(t, tt.wantSize, readRecordField(frame, "size"))
			assert.Equal(t, tt.wantLastActive, readRecordField(frame, "last active"))
		})
	}
}

// TestWorktreesRecordOccupants covers the record naming the other agents working in a directory.
func TestWorktreesRecordOccupants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		row       string
		occupants []string
		want      string
		wantAfter string
	}{
		{
			name: "none is left out",
			row:  "treeview",
		},
		{
			name:      "one is named after the repository",
			row:       "treeview",
			occupants: []string{"review-607"},
			want:      "review-607",
			wantAfter: "repository",
		},
		{
			name:      "two are named in order",
			row:       "treeview",
			occupants: []string{"second-reader", "review-607"},
			want:      "review-607, second-reader",
			wantAfter: "repository",
		},
		{
			name:      "an unreadable directory names them after its repository",
			row:       "broken",
			occupants: []string{"review-607"},
			want:      "review-607",
			wantAfter: "repository",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			all := buildWorktrees()
			rows := []proto.Message{all[0], all[1], all[3], all[4]}
			dir := "/trees/live"
			if tt.row == "broken" {
				dir = "/trees/broken"
			}
			for i, name := range tt.occupants {
				occupant := newAgent(fmt.Sprintf("occ%d", i), name, "w1")
				occupant.Cwd = dir
				rows = append(rows, occupant)
			}
			rows = append(rows, buildWatchedWorktrees()...)
			harness := openWorktrees(t, rows...)
			moveToWorktree(t, harness, tt.row)
			harness.press(widenListPresses...)
			harness.press("ctrl+t")

			frame := harness.readFrame()
			assert.Equal(t, tt.want, readRecordField(frame, "also here"))
			if tt.want != "" {
				assert.Equal(t, findRecordLine(frame, tt.wantAfter)+1, findRecordLine(frame, "also here"))
			}
		})
	}
}

// TestWorktreesDocument covers the document drawn beside the list.
func TestWorktreesDocument(t *testing.T) {
	t.Parallel()

	entries := []*worktreev1.Entry{
		{
			Path:      "local",
			SizeBytes: 40 * 1024 * 1024,
			Dir:       true,
		},
		{
			Path:      "go.mod",
			SizeBytes: 2048,
		},
	}
	for i := range 10 {
		entries = append(entries, &worktreev1.Entry{
			Path:      strings.Repeat("x", i+1),
			SizeBytes: 1024,
		})
	}

	t.Run("the contents, the changes and the commits since the base", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(&worktreev1.GetWorktreeResponse{
			Worktree:  &worktreev1.Worktree{Dir: "/trees/live"},
			Entries:   entries,
			Modified:  []string{"a.go"},
			Untracked: []string{"b.txt"},
			BaseRef:   "origin/main",
			Commits: []*worktreev1.Commit{{
				Sha:         "87a3bcc98c47",
				Subject:     "fix(view): drop the column",
				CommittedAt: stampAgo(2 * time.Hour),
			}},
		}))
		harness.fireTimers()

		document := readStageText(harness.readFrame())
		for _, want := range []string{
			"treeview",
			"local/      40M",
			"+ 2 more   2.0K",
			"M a.go",
			"? b.txt",
			"Commits since origin/main",
			"87a3bcc98c47  2h   fix(view): drop the column",
		} {
			assert.Contains(t, document, want)
		}
	})

	t.Run("a read git refused is drawn rather than asked again", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		harness.refuse(worktreev1connect.WorktreeServiceGetWorktreeProcedure, connect.NewError(connect.CodeInternal, errors.New("git exploded")))
		moveToWorktree(t, harness, "agent/orphan")
		harness.fireTimers()
		asked := len(harness.readSent(worktreev1connect.WorktreeServiceGetWorktreeProcedure))

		harness.watch(buildPuts(newMeasuredWorktree("/trees/admin", 1024)))

		assert.Contains(t, readStageText(harness.readFrame()), "git exploded")
		assert.Len(t, harness.readSent(worktreev1connect.WorktreeServiceGetWorktreeProcedure), asked)
	})
	t.Run("a reread refused keeps the last answer beside the refusal", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(&worktreev1.GetWorktreeResponse{
			Worktree: &worktreev1.Worktree{Dir: "/trees/live"},
			BaseRef:  "origin/main",
		}))
		harness.fireTimers()
		harness.refuse(worktreev1connect.WorktreeServiceGetWorktreeProcedure, connect.NewError(connect.CodeInternal, errors.New("git exploded")))

		harness.press("j", "k")
		harness.fireTimers()

		document := readStageText(harness.readFrame())
		assert.Contains(t, document, "could not read: git exploded")
		assert.Contains(t, document, "Commits since origin/main")
	})
}

// TestWorktreesInspection covers when the directory under the cursor is read.
func TestWorktreesInspection(t *testing.T) {
	t.Parallel()

	t.Run("the cursor coming to rest reads the directory it rests on, not the ones it passed", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		read := &worktreev1.GetWorktreeRequest{Dir: "/trees/live"}

		harness.press("g", "g", "j", "j", "j")
		harness.assertSentTo(worktreev1connect.WorktreeServiceGetWorktreeProcedure, read)
		harness.fireTimers()

		harness.assertSentTo(worktreev1connect.WorktreeServiceGetWorktreeProcedure, read, &worktreev1.GetWorktreeRequest{Dir: "/trees/archived"})
	})

	t.Run("a listing that moves leaves the cursor on its row", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		moveToWorktree(t, harness, "agent/orphan")

		harness.watch(buildSnapshot(append(buildWorktrees()[:5], buildWatchedWorktrees()[1:]...)...))

		assert.Equal(t, "agent/orphan", readHoveredWorktree(harness.readFrame()))
	})
}

func openWorktrees(t *testing.T, rows ...proto.Message) *panelHarness {
	t.Helper()

	if len(rows) == 0 {
		rows = buildWorktrees()
	}

	harness := openAgents(t, rows...)
	harness.press("t")

	return harness
}

func buildWorktrees() []proto.Message {
	live := newAgent("live", "treeview", "w1")
	live.Harness = "claude"
	live.Cwd = "/trees/live"
	live.Alive = proto.Bool(true)
	live.State = agentv1.AgentState_AGENT_STATE_THINKING

	occupant := newAgent("occ", "review-607", "w1")
	occupant.Cwd = "/trees/live"
	occupant.Alive = proto.Bool(true)

	archived := newArchivedAgent("archived", "reviewer", "w1")
	archived.Harness = "claude"
	archived.Cwd = "/trees/archived"

	admin := newArchivedAgent("admin", "cargo-fix", "w1")
	admin.Cwd = "/trees/admin"

	return append([]proto.Message{
		newLiveWorkflow("w1", "weave"),
		live,
		occupant,
		archived,
		admin,
	}, buildWatchedWorktrees()...)
}

func buildWatchedWorktrees() []proto.Message {
	return []proto.Message{
		&worktreev1.WatchedWorktree{
			Dir:      "/trees/admin",
			RepoDir:  "/home/h/gadgets/admin",
			Branch:   "agent/admin",
			Readable: true,
		},
		&worktreev1.WatchedWorktree{
			Dir:      "/trees/archived",
			RepoDir:  "/home/h/weave",
			Branch:   "agent/archived",
			Readable: true,
		},
		&worktreev1.WatchedWorktree{
			Dir:     "/trees/broken",
			RepoDir: "/var/folders/T/repo",
		},
		&worktreev1.WatchedWorktree{
			Dir:      "/trees/live",
			RepoDir:  "/home/h/weave",
			Branch:   "agent/live",
			Readable: true,
		},
		&worktreev1.WatchedWorktree{
			Dir:      "/trees/orphan",
			RepoDir:  "/home/h/weave",
			Branch:   "agent/orphan",
			Readable: true,
		},
	}
}

func newMeasuredWorktree(dir string, size int64) *worktreev1.WatchedWorktree {
	for _, row := range buildWatchedWorktrees() {
		worktree, _ := row.(*worktreev1.WatchedWorktree)
		if worktree.GetDir() == dir {
			worktree.SizeBytes = proto.Int64(size)
			worktree.MeasuredAt = proto.String(stampAgo(time.Minute))

			return worktree
		}
	}

	return nil
}

func moveToWorktree(t *testing.T, harness *panelHarness, name string) {
	t.Helper()

	harness.press("g", "g")
	for range 10 {
		if readHoveredWorktree(harness.readFrame()) == name {
			return
		}
		harness.press("j")
	}

	require.Equal(t, name, readHoveredWorktree(harness.readFrame()))
}

func readHoveredWorktree(frame string) string {
	return strings.TrimSpace(readHoveredRow(frame))
}

func stampAgo(ago time.Duration) string {
	return time.Now().Add(-ago).UTC().Format(time.RFC3339)
}

func readFormProse(frame string) string {
	lines := xslice.Map(readStage(frame), func(line string) string {
		return strings.TrimPrefix(strings.TrimSpace(line), "┃")
	})

	return strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
}
