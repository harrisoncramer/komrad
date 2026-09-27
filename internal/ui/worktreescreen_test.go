package ui_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"komrad/internal/ui/paint"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	worktreev1 "komrad/proto/gen/worktree/v1"
	"komrad/proto/gen/worktree/v1/worktreev1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

const worktreesScreen = "the worktrees"

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
			press:       []string{"o", "w"},
			wantHovered: "treeview",
		},
		{
			name:        "an occupant lands on the directory it works in",
			press:       []string{"j", "o", "w"},
			wantHovered: "treeview",
		},
		{
			name:        "from the archive it lands on the archived agent's directory",
			press:       []string{"o", "r", "j", "o", "w"},
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
			assert.Equal(t, worktreesScreen, harness.readScreen())
			assert.Equal(t, tt.wantHovered, readHoveredWorktree(frame))
		})
	}

	t.Run("the panel keeps the whole window, a live agent's pane included", func(t *testing.T) {
		t.Parallel()

		rows := buildWorktrees()
		rows[1].(*agentv1.Agent).PaneId = "%7"
		harness := openAgents(t, rows...)
		harness.press("o", "w")

		assert.True(t, harness.panel.IsZoomed())
	})

	t.Run("the title counts the directories and totals what has been measured", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		require.Contains(t, readBelowViews(harness.readFrame())[0], "5 · —")

		harness.watch(buildPuts(
			newMeasuredWorktree("/trees/live", 49*1024*1024),
			newMeasuredWorktree("/trees/archived", 49*1024*1024),
			newMeasuredWorktree("/trees/admin", 1536*1024*1024),
		))

		assert.Contains(t, readBelowViews(harness.readFrame())[0], "5 · 1.6G")
	})
}

// TestWorktreesJump covers jumping from a directory to the agent it belongs to.
func TestWorktreesJump(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		row         string
		wantScreen  string
		wantHovered string
	}{
		{
			name:        "a directory nothing answers for stays put",
			row:         "agent/orphan",
			wantScreen:  worktreesScreen,
			wantHovered: "agent/orphan",
		},
		{
			name:        "a live agent's directory lands on the agents list",
			row:         "treeview",
			wantScreen:  agentsScreen,
			wantHovered: "treeview",
		},
		{
			name:        "an archived agent's directory lands in the archive",
			row:         "reviewer",
			wantScreen:  archiveScreen,
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
			assert.Equal(t, tt.wantScreen, harness.readScreen())
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
			row:        "/home/h/komrad",
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

	t.Run("a directory git cannot read after a failed cleanup is deleted on its own", func(t *testing.T) {
		t.Parallel()

		stuck := newArchivedAgent("stuck", "stuck", "w1")
		stuck.State = agentv1.AgentState_AGENT_STATE_FAILED_TO_CLEANUP
		stuck.Cwd = "/trees/stuck"
		harness := openWorktrees(t,
			newLiveWorkflow("w1", "komrad"),
			stuck,
			&worktreev1.WatchedWorktree{Dir: "/trees/stuck"},
		)
		moveToWorktree(t, harness, "stuck")

		harness.press("d")

		assert.Contains(t, readFormProse(harness.readFrame()), "The directory nothing answers for is removed on its own")
	})
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

	t.Run("the listing names the agent through the list that holds it", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		moveToWorktree(t, harness, "reviewer")
		harness.press(widenListPresses...)

		frame := harness.readFrame()
		assert.Equal(t, "reviewer", readRecordField(frame, "name"))
		assert.Equal(t, "—", readRecordField(frame, "size"))
		assert.Equal(t, "agent/archived", readRecordField(frame, "branch"))
		assert.Equal(t, "komrad", readRecordField(frame, "workflow"))
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
		raw := harness.readRawFrame()
		assert.Contains(t, raw, paint.Panel.Style(paint.ToneSuccess).Render("↑2"))
		assert.Contains(t, raw, paint.Panel.Style(paint.ToneMark).Render("↓12"))
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
			name: "nothing measured and nothing touched",
			worktree: &worktreev1.WatchedWorktree{
				Dir:      "/trees/orphan",
				Readable: true,
				Branch:   "agent/orphan",
			},
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
				MeasuredAt: proto.String(formatStampAgo(5 * time.Minute)),
				SizeError:  "permission denied",
				ModifiedAt: formatStampAgo(90 * time.Minute),
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
				MeasuredAt: proto.String(formatStampAgo(5 * time.Minute)),
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
			rows[3].(*agentv1.Agent).ArchivedAt = proto.String(formatStampAgo(4 * 24 * time.Hour))
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
		dir       string
		occupants []string
		want      string
		wantAfter string
	}{
		{
			name: "none is left out",
			row:  "treeview",
			dir:  "/trees/live",
		},
		{
			name:      "an unreadable directory names them after its repository",
			row:       "broken",
			dir:       "/trees/broken",
			occupants: []string{"review-607"},
			want:      "review-607",
			wantAfter: "repository",
		},
		{
			name:      "one is named after the repository",
			row:       "treeview",
			dir:       "/trees/live",
			occupants: []string{"review-607"},
			want:      "review-607",
			wantAfter: "repository",
		},
		{
			name:      "two are named in order",
			row:       "treeview",
			dir:       "/trees/live",
			occupants: []string{"second-reader", "review-607"},
			want:      "review-607, second-reader",
			wantAfter: "repository",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			all := buildWorktrees()
			rows := []proto.Message{all[0], all[1], all[3], all[4]}
			for i, name := range tt.occupants {
				occupant := newAgent(fmt.Sprintf("occ%d", i), name, "w1")
				occupant.Cwd = tt.dir
				rows = append(rows, occupant)
			}
			rows = append(rows, buildWatchedWorktrees()...)
			harness := openWorktrees(t, rows...)
			moveToWorktree(t, harness, tt.row)
			harness.press(widenListPresses...)
			harness.press("ctrl+a")

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
				CommittedAt: formatStampAgo(2 * time.Hour),
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

	t.Run("a directory whose read is on its way is not asked for again", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		harness.hold(worktreev1connect.WorktreeServiceGetWorktreeProcedure)
		moveToWorktree(t, harness, "reviewer")
		harness.fireTimers()

		harness.press("j", "k")
		harness.fireTimers()

		assert.Len(t, readWorktreeReadsOf(harness, "/trees/archived"), 1)
	})

	t.Run("a row read again shows its last answer until the new one lands", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		harness.answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, answerWith(&worktreev1.GetWorktreeResponse{
			Worktree: &worktreev1.Worktree{
				Dir:      "/trees/archived",
				Readable: true,
			},
			BaseRef: "origin/main",
			Ahead:   2,
		}))
		moveToWorktree(t, harness, "reviewer")
		harness.fireTimers()
		harness.hold(worktreev1connect.WorktreeServiceGetWorktreeProcedure)

		harness.press("j", "k")
		harness.fireTimers()

		frame := harness.readFrame()
		assert.Contains(t, frame, "Commits since origin/main")
		assert.NotContains(t, frame, "reading the directory")
	})
}

// TestWorktreesSort covers the strip reordering the directories under the cursor.
func TestWorktreesSort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		press []string
		want  []string
	}{
		{
			name: "size down is the fallback, under each repository",
			want: []string{"/home/h/gadgets/admin", "  cargo-fix", "/home/h/komrad", "  reviewer", "  treeview", "  agent/orphan", "/var/folders/T/repo", "  broken"},
		},
		{
			name:  "moving the focus reorders nothing",
			press: []string{"f", "tab", "esc"},
			want:  []string{"/home/h/gadgets/admin", "  cargo-fix", "/home/h/komrad", "  reviewer", "  treeview", "  agent/orphan", "/var/folders/T/repo", "  broken"},
		},
		{
			name:  "last accessed down is the most recent first",
			press: []string{"f", "tab", "j", "esc"},
			want:  []string{"/home/h/gadgets/admin", "  cargo-fix", "/home/h/komrad", "  treeview", "  agent/orphan", "  reviewer", "/var/folders/T/repo", "  broken"},
		},
		{
			name:  "the stop drops the headings and settles a tie in size by path",
			press: []string{"f", "shift+tab", "j", "esc"},
			want:  []string{"cargo-fix", "reviewer", "treeview", "broken", "agent/orphan"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openWorktrees(t, buildMeasuredWorktrees()...)

			harness.press(tt.press...)

			frame := harness.readFrame()
			assert.Equal(t, tt.want, readListRows(frame))
			assert.Equal(t, "treeview", readHoveredWorktree(frame), "the cursor keeps its row")
		})
	}

	t.Run("the arrow turns under the cursor and the list takes the keys back", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t, buildMeasuredWorktrees()...)
		harness.press("f", "tab", "j", "f", "k")

		frame := harness.readFrame()
		assert.Contains(t, readSidebar(frame)[3], "↑ last-accessed", "the focus key is not the way back")
		assert.Equal(t, "treeview", readRecordField(frame, "name"), "the cursor follows its row as the arrow turns")

		harness.press("esc", "j")
		assert.Equal(t, "/var/folders/T/repo", readHoveredWorktree(harness.readFrame()))
	})

	t.Run("the stop flips the headings back and escape keeps what it chose", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t, buildMeasuredWorktrees()...)
		harness.press("f", "tab", "tab", "j")
		require.Contains(t, readBelowViews(harness.readFrame())[0], "flat")

		harness.press("k")
		frame := harness.readFrame()
		assert.NotContains(t, readBelowViews(frame)[0], "flat")
		assert.Equal(t, "treeview", readRecordField(frame, "name"))

		harness.press("j", "esc")
		assert.Contains(t, readBelowViews(harness.readFrame())[0], "flat")
	})

	t.Run("a cursor on a heading lands on a row once the headings go", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t, buildMeasuredWorktrees()...)
		harness.press("k", "k")
		require.Equal(t, "/home/h/komrad", readHoveredWorktree(harness.readFrame()))

		harness.press("f", "shift+tab", "j", "esc")

		assert.Equal(t, "treeview", readHoveredWorktree(harness.readFrame()))
	})

	t.Run("the strip left from the stop comes back on its chosen column", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t, buildMeasuredWorktrees()...)
		harness.press("f", "shift+tab", "esc", "f", "k")

		frame := harness.readFrame()
		assert.Contains(t, readSidebar(frame)[3], "↑ size")
		assert.NotContains(t, readBelowViews(frame)[0], "flat")
	})
}

func openWorktrees(t *testing.T, rows ...proto.Message) *panelHarness {
	t.Helper()

	if len(rows) == 0 {
		rows = buildWorktrees()
	}

	harness := openAgents(t, rows...)
	harness.press("o", "w")

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
		newLiveWorkflow("w1", "komrad"),
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
			RepoDir:  "/home/h/komrad",
			Branch:   "agent/archived",
			Readable: true,
		},
		&worktreev1.WatchedWorktree{
			Dir:     "/trees/broken",
			RepoDir: "/var/folders/T/repo",
		},
		&worktreev1.WatchedWorktree{
			Dir:      "/trees/live",
			RepoDir:  "/home/h/komrad",
			Branch:   "agent/live",
			Readable: true,
		},
		&worktreev1.WatchedWorktree{
			Dir:      "/trees/orphan",
			RepoDir:  "/home/h/komrad",
			Branch:   "agent/orphan",
			Readable: true,
		},
	}
}

func buildMeasuredWorktrees() []proto.Message {
	rows := buildWorktrees()
	for _, row := range rows {
		switch row := row.(type) {
		case *agentv1.Agent:
			measureAgent(row)
		case *worktreev1.WatchedWorktree:
			measureWorktree(row)
		}
	}

	return rows
}

func measureAgent(agent *agentv1.Agent) {
	switch agent.GetId() {
	case "live":
		agent.StateChangedAt = formatStampAgo(5 * time.Minute)
	case "archived":
		agent.ArchivedAt = proto.String(formatStampAgo(4 * 24 * time.Hour))
	case "admin":
		agent.ArchivedAt = proto.String(formatStampAgo(12 * time.Hour))
	}
}

func measureWorktree(worktree *worktreev1.WatchedWorktree) {
	switch worktree.GetDir() {
	case "/trees/live", "/trees/archived":
		worktree.SizeBytes = proto.Int64(49 * 1024 * 1024)
		worktree.MeasuredAt = proto.String(formatStampAgo(time.Hour))
	case "/trees/orphan":
		worktree.MeasuredAt = proto.String(formatStampAgo(time.Hour))
		worktree.SizeError = "permission denied"
		worktree.ModifiedAt = formatStampAgo(3 * time.Hour)
	case "/trees/broken":
		worktree.ModifiedAt = formatStampAgo(2 * 7 * 24 * time.Hour)
	case "/trees/admin":
		worktree.SizeBytes = proto.Int64(1536 * 1024 * 1024)
		worktree.MeasuredAt = proto.String(formatStampAgo(time.Hour))
	}
}

func newMeasuredWorktree(dir string, size int64) *worktreev1.WatchedWorktree {
	for _, row := range buildWatchedWorktrees() {
		worktree, _ := row.(*worktreev1.WatchedWorktree)
		if worktree.GetDir() == dir {
			worktree.SizeBytes = proto.Int64(size)
			worktree.MeasuredAt = proto.String(formatStampAgo(time.Minute))

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

func readWorktreeReadsOf(harness *panelHarness, dir string) []proto.Message {
	return slices.DeleteFunc(harness.readSent(worktreev1connect.WorktreeServiceGetWorktreeProcedure), func(sent proto.Message) bool {
		read, _ := sent.(*worktreev1.GetWorktreeRequest)

		return read.GetDir() != dir
	})
}

func readHoveredWorktree(frame string) string {
	return strings.TrimSpace(readHoveredRow(frame))
}

func formatStampAgo(ago time.Duration) string {
	return time.Now().Add(-ago).UTC().Format(time.RFC3339)
}

func readFormProse(frame string) string {
	lines := xslice.Map(readStage(frame), func(line string) string {
		return strings.TrimPrefix(strings.TrimSpace(line), "┃")
	})

	return joinProse(lines)
}
