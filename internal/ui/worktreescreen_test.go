package ui_test

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"komrad/internal/ui/paint"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	taskv1 "komrad/proto/gen/task/v1"
	"komrad/proto/gen/task/v1/taskv1connect"
	worktreev1 "komrad/proto/gen/worktree/v1"
	"komrad/proto/gen/worktree/v1/worktreev1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

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
			press:       []string{"shift+tab", "esc"},
			wantHovered: "treeview",
		},
		{
			name:        "a task lands on its writer's directory",
			press:       []string{"j", "shift+tab", "esc"},
			wantHovered: "treeview",
		},
		{
			name:        "an occupant lands on the directory it works in",
			press:       []string{"j", "j", "shift+tab", "esc"},
			wantHovered: "treeview",
		},
		{
			name:        "from the archived agents it lands on the archived agent's directory",
			press:       []string{"F", "tab", "esc", "shift+tab", "esc"},
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
		harness.press("shift+tab")

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
			row:         "task/orphan",
			wantScreen:  worktreesScreen,
			wantHovered: "task/orphan",
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
			wantScreen:  agentsScreen,
			wantHovered: "reviewer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openWorktrees(t)
			moveToWorktree(t, harness, tt.row)

			harness.press("space", "J", "a")

			frame := harness.readFrame()
			assert.Equal(t, tt.wantScreen, harness.readScreen())
			assert.Contains(t, readHoveredRow(frame), tt.wantHovered)
		})
	}
}

// TestWorktreesDelete covers what each delete entry opens on the row under the cursor.
func TestWorktreesDelete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		row          string
		press        []string
		wantRendered string
		wantAbsent   string
	}{
		{
			name:       "a repository header opens nothing",
			row:        "/home/h/komrad",
			press:      []string{"space", "a", "d"},
			wantAbsent: "Delete",
		},
		{
			name:         "a live task's directory is offered the task's archive",
			row:          "treeview",
			press:        []string{"space", "a", "a"},
			wantRendered: `archiving task "treeview"`,
		},
		{
			name:         "an archived task's directory is offered the task delete",
			row:          "reviewer",
			press:        []string{"space", "a", "d"},
			wantRendered: `Permanently delete task "reviewer"?`,
		},
		{
			name:         "a directory nothing answers for is offered the delete on its own",
			row:          "broken",
			press:        []string{"space", "a", "d"},
			wantRendered: "Delete the worktree of broken? The directory nothing answers for is removed on its own",
		},
		{
			name:         "a selection of two directories nothing answers for counts them",
			row:          "task/orphan",
			press:        []string{"v", "j", "v", "space", "a", "d"},
			wantRendered: "Delete 2 worktrees? 2 directories nothing answers for are removed on their own",
		},
		{
			name:         "clear gathers only the directories no task answers for",
			row:          "treeview",
			press:        []string{"space", "a", "c"},
			wantRendered: "Delete 2 worktrees?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openGroupedWorktrees(t)
			moveToWorktree(t, harness, tt.row)

			harness.press(tt.press...)

			frame := harness.readFrame()
			prose := readFormProse(frame) + " " + readSidebarProse(frame)
			assert.Contains(t, prose, tt.wantRendered)
			if tt.wantAbsent != "" {
				assert.NotContains(t, prose, tt.wantAbsent)
			}
		})
	}

	t.Run("clear is gray and asks nothing when only live directories are listed", func(t *testing.T) {
		t.Parallel()

		live := newAgent("live", "treeview", "w1")
		live.Cwd = "/trees/live"
		live.Alive = xptr.Ptr(true)
		harness := openWorktrees(t,
			newLiveWorkflow("w1", "komrad"),
			live,
			newWorktreeTask(live),
			&worktreev1.WatchedWorktree{
				Dir:      "/trees/live",
				RepoDir:  "/home/h/komrad",
				Branch:   "task/live",
				Readable: true,
			},
		)
		moveToWorktree(t, harness, "treeview")

		harness.press("space", "a")
		assert.Regexp(t, readOffEntry("clear what can go", "c"), harness.readRawFrame())

		harness.press("c", "y")

		assert.NotContains(t, readFormProse(harness.readFrame()), "Delete")
		harness.assertSentTo(worktreev1connect.WorktreeServiceDeleteWorktreesProcedure)
	})

	t.Run("chosen task checkouts are deleted with their tasks", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t, buildTaskWorktrees()...)
		moveToWorktree(t, harness, "alpha")

		harness.press("v", "j", "v", "space", "a", "d", "y")

		harness.assertSentTo(taskv1connect.TaskServiceDeleteTasksProcedure, &taskv1.DeleteTasksRequest{Tasks: []string{"t8", "t9"}})
	})

	t.Run("a choice mixing a task checkout with the rest says why it deletes nothing", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t, buildTaskWorktrees()...)
		moveToWorktree(t, harness, "beta")

		harness.press("v", "j", "v", "space", "a", "d")

		assert.Contains(t, harness.readFrame(), "Could not delete 2 worktrees")
		harness.assertSentTo(taskv1connect.TaskServiceDeleteTasksProcedure)
		harness.assertSentTo(worktreev1connect.WorktreeServiceDeleteWorktreesProcedure)
	})
}

// TestWorktreesMenu covers the entry the archive menu offers for each kind of row.
func TestWorktreesMenu(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		row   string
		press []string
		label string
		key   string
	}{
		{
			name:  "a live task's directory offers its archive",
			row:   "treeview",
			label: "archive the task",
			key:   "a",
		},
		{
			name:  "an archived task's directory offers its delete",
			row:   "reviewer",
			label: "delete the task",
			key:   "d",
		},
		{
			name:  "a directory nothing answers for offers the remover",
			row:   "broken",
			label: "remove",
			key:   "d",
		},
		{
			name:  "a selection offers to delete it all",
			row:   "treeview",
			press: []string{"v", "j", "v"},
			label: "delete all",
			key:   "d",
		},
		{
			name:  "a repository header offers only the clear",
			row:   "/home/h/komrad",
			label: "clear what can go",
			key:   "c",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openGroupedWorktrees(t)
			moveToWorktree(t, harness, tt.row)

			harness.press(append(tt.press, "space", "a")...)

			entry := regexp.MustCompile(`(?m)› ` + regexp.QuoteMeta(tt.label) + ` +` + tt.key + `\s*$`)
			assert.Regexp(t, entry, harness.readFrame())
		})
	}
}

// TestWorktreesDeleteAnswer covers answering the prompt the clear entry opens.
func TestWorktreesDeleteAnswer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		refusal  error
		answer   string
		wantSent []proto.Message
		gone     []string
		want     string
	}{
		{
			name:     "a queue that refuses is reported",
			refusal:  errors.New("the queue is away"),
			answer:   "y",
			wantSent: []proto.Message{&worktreev1.DeleteWorktreesRequest{Dirs: []string{"/trees/broken", "/trees/orphan"}}},
			want:     "Could not delete 2 worktrees",
		},
		{
			name:   "declining queues nothing",
			answer: "n",
			want:   "local",
		},
		{
			name:     "confirming queues every directory that can go in one call",
			answer:   "y",
			wantSent: []proto.Message{&worktreev1.DeleteWorktreesRequest{Dirs: []string{"/trees/broken", "/trees/orphan"}}},
			gone:     []string{"/trees/broken", "/trees/orphan"},
			want:     "Deleting 2 worktrees",
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

			harness.press("space", "a", "c", tt.answer)
			if len(tt.gone) > 0 {
				harness.watch(buildDeletedWorktrees(tt.gone...))
			}

			harness.assertSentTo(worktreev1connect.WorktreeServiceDeleteWorktreesProcedure, tt.wantSent...)
			frame := harness.readFrame()
			assert.True(t, strings.HasSuffix(readEngineLine(frame), tt.want), readEngineLine(frame))
			assert.NotContains(t, frame, "Delete 2 worktrees?")
		})
	}
}

// TestWorktreesRecord covers the record the list renders for a directory.
func TestWorktreesRecord(t *testing.T) {
	t.Parallel()

	t.Run("a directory git cannot read names its repository and no task", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		moveToWorktree(t, harness, "broken")
		harness.press(widenListPresses...)

		frame := harness.readFrame()
		assert.Equal(t, "/var/folders/T/repo", readRecordField(frame, "repository"))
		assert.Equal(t, "none", readRecordField(frame, "task"))
		assert.Empty(t, readRecordField(frame, "agents"))
		assert.Empty(t, readRecordField(frame, "branch"))
	})

	t.Run("the listing names the task and the agents working in its directory", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		moveToWorktree(t, harness, "reviewer")
		harness.press(widenListPresses...)

		frame := harness.readFrame()
		assert.Equal(t, "reviewer", readRecordField(frame, "name"))
		assert.Equal(t, "—", readRecordField(frame, "size"))
		assert.Equal(t, "task/archived", readRecordField(frame, "branch"))
		assert.Equal(t, "komrad", readRecordField(frame, "workflow"))
		assert.Equal(t, "reviewer", readRecordField(frame, "task"))
		assert.Equal(t, "reviewer", readRecordField(frame, "agents"))
		assert.Empty(t, readRecordField(frame, "base"))
	})

	t.Run("a directory named after its task names the agents provisioned for the task and those working in it", func(t *testing.T) {
		t.Parallel()

		writer := newAgent("writer-id", "writer", "w1")
		writer.Cwd = "/home/h/komrad"
		task := newTaskOf(writer)
		reviewer := newAgent("reviewer-id", "reviewer", "w1")
		reviewer.Cwd = "/trees/" + task.GetId()
		reviewer.TaskId = writer.TaskId
		reviewer.ParentAgentId = xptr.Ptr(writer.GetId())
		adopter := newAgent("adopter-id", "adopter", "w1")
		adopter.Cwd = writer.GetCwd()
		adopter.TaskId = writer.TaskId
		adopter.ProvisionDisabled = true

		harness := openWorktrees(t,
			newLiveWorkflow("w1", "komrad"),
			writer,
			reviewer,
			adopter,
			task,
			&worktreev1.WatchedWorktree{
				Dir:      reviewer.GetCwd(),
				RepoDir:  "/home/h/komrad",
				Branch:   "task/" + task.GetId(),
				Readable: true,
			},
		)
		moveToWorktree(t, harness, task.GetSlug())
		harness.press(widenListPresses...)
		harness.press("pick:record")

		assert.Equal(t, "writer, reviewer", readRecordField(harness.readFrame(), "agents"))
	})

	t.Run("a live agent provisioned for the task is named before a newer archived one", func(t *testing.T) {
		t.Parallel()

		writer := newAgent("writer-id", "writer", "w1")
		writer.CreatedAt = "2026-09-20T12:00:00Z"
		task := newTaskOf(writer)
		writer.Cwd = "/trees/" + task.GetId()
		rival := newArchivedAgent("rival-id", "rival", "w1")
		rival.CreatedAt = "2026-09-20T13:00:00Z"
		rival.Cwd = "/home/h/komrad"
		rival.TaskId = writer.TaskId

		harness := openWorktrees(t,
			newLiveWorkflow("w1", "komrad"),
			writer,
			rival,
			task,
			&worktreev1.WatchedWorktree{
				Dir:      writer.GetCwd(),
				RepoDir:  "/home/h/komrad",
				Branch:   "task/" + task.GetId(),
				Readable: true,
			},
		)
		moveToWorktree(t, harness, task.GetSlug())
		harness.press(widenListPresses...)
		harness.press("pick:record")

		assert.Equal(t, "writer, rival", readRecordField(harness.readFrame(), "agents"))
	})

	t.Run("a task's checkout no agent answers for names its task", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t,
			newLiveWorkflow("w1", "komrad"),
			&taskv1.Task{Id: "t9", WorkflowId: "w1", Slug: "fix-login"},
			&worktreev1.WatchedWorktree{
				Dir:      "/trees/t9",
				RepoDir:  "/home/h/komrad",
				Branch:   "task/t9",
				Readable: true,
			},
		)
		moveToWorktree(t, harness, "fix-login")
		harness.press(widenListPresses...)

		frame := harness.readFrame()
		assert.Equal(t, "fix-login", readRecordField(frame, "task"))
		assert.Empty(t, readRecordField(frame, "agents"))
	})

	t.Run("the read of the directory adds where the branch stands and what it changed", func(t *testing.T) {
		t.Parallel()

		harness := openReadWorktree(t)
		harness.press(widenListPresses...)

		frame := harness.readFrame()
		assert.Equal(t, "origin/main  ↑2 ↓12", readRecordField(frame, "base"))
		assert.Equal(t, "1 modified, 2 untracked", readRecordField(frame, "changes"))
		raw := harness.readRawFrame()
		assert.Contains(t, raw, paint.Panel.Style(paint.ToneSuccess).Render("↑2"))
		assert.Contains(t, raw, paint.Panel.Style(paint.ToneWarning).Render("↓12"))
	})

	t.Run("a coloured value is copied as its plain text", func(t *testing.T) {
		t.Parallel()

		harness := openReadWorktree(t)
		harness.press("pick:record", "/", "m", "a", "i", "n", "enter", "y")

		assert.Equal(t, []string{"origin/main  ↑2 ↓12"}, harness.panel.ReadCopied())
	})

	t.Run("a search over a coloured value lights its match in the plain text", func(t *testing.T) {
		t.Parallel()

		harness := openReadWorktree(t)
		harness.press(append([]string{"pick:record", "/", "m", "a", "i", "n", "enter"}, widenListPresses...)...)

		line := findRawLine(harness.readRawFrame(), regexp.MustCompile(`base\s+origin/main`))
		assert.Contains(t, line, paint.Panel.Match().Render("main"))
		assert.NotContains(t, line, paint.Panel.Style(paint.ToneSuccess).Render("↑2"))
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
				Branch:   "task/orphan",
			},
			row:            "task/orphan",
			wantSize:       "—",
			wantLastActive: "—",
		},
		{
			name: "a failed walk says why",
			worktree: &worktreev1.WatchedWorktree{
				Dir:        "/trees/orphan",
				Readable:   true,
				Branch:     "task/orphan",
				MeasuredAt: xptr.Ptr(formatStampAgo(5 * time.Minute)),
				SizeError:  "permission denied",
				ModifiedAt: formatStampAgo(90 * time.Minute),
			},
			row:            "task/orphan",
			wantSize:       "—, could not measure: permission denied",
			wantLastActive: "1h ago",
		},
		{
			name: "a measured directory whose agent was put away",
			worktree: &worktreev1.WatchedWorktree{
				Dir:        "/trees/archived",
				Readable:   true,
				Branch:     "task/archived",
				SizeBytes:  xptr.Ptr[int64](49 * 1024 * 1024),
				MeasuredAt: xptr.Ptr(formatStampAgo(90 * time.Minute)),
			},
			row:            "reviewer",
			wantSize:       "49M, measured 1h ago",
			wantLastActive: "4d ago, archived",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rows := buildWorktrees()
			rows[3].(*agentv1.Agent).ArchivedAt = xptr.Ptr(formatStampAgo(4 * 24 * time.Hour))
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

// TestWorktreesRecordOccupants covers the record naming the agents working in a directory.
func TestWorktreesRecordOccupants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		row       string
		dir       string
		occupants []string
		want      string
	}{
		{
			name: "a directory nobody works in names none",
			row:  "task/orphan",
			dir:  "/trees/orphan",
		},
		{
			name:      "an unreadable directory names them",
			row:       "broken",
			dir:       "/trees/broken",
			occupants: []string{"review-607"},
			want:      "review-607",
		},
		{
			name: "the agent the task was cut for is named",
			row:  "treeview",
			dir:  "/trees/live",
			want: "treeview",
		},
		{
			name:      "every agent is named, newer first",
			row:       "treeview",
			dir:       "/trees/live",
			occupants: []string{"second-reader", "review-607"},
			want:      "review-607, second-reader, treeview",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			all := buildWorktrees()
			rows := []proto.Message{all[0], all[1], all[3], all[4], all[5], all[6], all[7]}
			for i, name := range tt.occupants {
				occupant := newAgent(fmt.Sprintf("occ%d", i), name, "w1")
				occupant.Cwd = tt.dir
				occupant.CreatedAt = fmt.Sprintf("2026-09-20T1%d:00:00Z", i)
				rows = append(rows, occupant)
			}
			rows = append(rows, buildWatchedWorktrees()...)
			harness := openWorktrees(t, rows...)
			moveToWorktree(t, harness, tt.row)
			harness.press(widenListPresses...)
			harness.press("pick:record")

			assert.Equal(t, tt.want, readRecordField(harness.readFrame(), "agents"))
		})
	}
}

// TestWorktreesDocument covers the document rendered beside the list.
func TestWorktreesDocument(t *testing.T) {
	t.Parallel()

	t.Run("a read git refused is rendered rather than asked again", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		harness.refuse(worktreev1connect.WorktreeServiceGetWorktreeProcedure, connect.NewError(connect.CodeInternal, errors.New("git exploded")))
		moveToWorktree(t, harness, "task/orphan")
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

// TestWorktreesLastScreen covers the owner's saved screen rendered under a worktree.
func TestWorktreesLastScreen(t *testing.T) {
	t.Parallel()

	t.Run("the owner's saved screen is rendered under the git blocks", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		answerLastScreens(harness, map[string]string{"archived": "⏺ the reviewer approved"})

		moveToWorktree(t, harness, "reviewer")
		harness.fireTimers()

		document := readStageText(harness.readFrame())
		assert.Contains(t, document, "saved screen")
		assert.Contains(t, document, "the reviewer approved")
	})

	t.Run("a worktree with no owner has no saved screen", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		answerLastScreens(harness, nil)

		moveToWorktree(t, harness, "task/orphan")
		harness.fireTimers()

		assert.NotContains(t, readStageText(harness.readFrame()), "saved screen")
	})
}

// TestWorktreesInspection covers when the directory under the cursor is read.
func TestWorktreesInspection(t *testing.T) {
	t.Parallel()

	t.Run("the cursor coming to rest reads the directory it rests on, not the ones it passed", func(t *testing.T) {
		t.Parallel()

		harness := openGroupedWorktrees(t)

		harness.press("g", "g", "j", "j", "j")
		harness.assertSentTo(worktreev1connect.WorktreeServiceGetWorktreeProcedure)
		harness.fireTimers()

		harness.assertSentTo(worktreev1connect.WorktreeServiceGetWorktreeProcedure, &worktreev1.GetWorktreeRequest{Dir: "/trees/archived"})
	})

	t.Run("a listing that moves leaves the cursor on its row", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		moveToWorktree(t, harness, "task/orphan")

		harness.watch(buildSnapshot(append(buildWorktrees()[:5], buildWatchedWorktrees()[1:]...)...))

		assert.Equal(t, "task/orphan", readHoveredWorktree(harness.readFrame()))
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
			want: []string{"/home/h/gadgets/admin", "  cargo-fix", "/home/h/komrad", "  reviewer", "  treeview", "  task/orphan", "/var/folders/T/repo", "  broken"},
		},
		{
			name:  "moving the focus reorders nothing",
			press: []string{"f", "tab", "esc"},
			want:  []string{"/home/h/gadgets/admin", "  cargo-fix", "/home/h/komrad", "  reviewer", "  treeview", "  task/orphan", "/var/folders/T/repo", "  broken"},
		},
		{
			name:  "last accessed down is the most recent first",
			press: []string{"f", "tab", "j", "esc"},
			want:  []string{"/home/h/gadgets/admin", "  cargo-fix", "/home/h/komrad", "  treeview", "  task/orphan", "  reviewer", "/var/folders/T/repo", "  broken"},
		},
		{
			name:  "the stop drops the headings and settles a tie in size by path",
			press: []string{"f", "shift+tab", "j", "esc"},
			want:  []string{"cargo-fix", "reviewer", "treeview", "broken", "task/orphan"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openGroupedWorktrees(t, buildMeasuredWorktrees()...)

			harness.press(tt.press...)

			frame := harness.readFrame()
			assert.Equal(t, tt.want, readListRows(frame))
			assert.Equal(t, "treeview", readHoveredWorktree(frame), "the cursor keeps its row")
		})
	}

	t.Run("the arrow turns under the cursor and the list takes the keys back", func(t *testing.T) {
		t.Parallel()

		harness := openGroupedWorktrees(t, buildMeasuredWorktrees()...)
		harness.press("f", "tab", "j", "f", "k")

		frame := harness.readFrame()
		assert.Contains(t, readSidebar(frame)[1], "↑ last-accessed", "the focus key is not the way back")
		assert.Equal(t, "treeview", readRecordField(frame, "name"), "the cursor follows its row as the arrow turns")

		harness.press("esc", "j")
		assert.Equal(t, "/var/folders/T/repo", readHoveredWorktree(harness.readFrame()))
	})

	t.Run("the stop flips the headings back and escape keeps what it chose", func(t *testing.T) {
		t.Parallel()

		harness := openGroupedWorktrees(t, buildMeasuredWorktrees()...)
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

		harness := openGroupedWorktrees(t, buildMeasuredWorktrees()...)
		harness.press("k", "k")
		require.Equal(t, "/home/h/komrad", readHoveredWorktree(harness.readFrame()))

		harness.press("f", "shift+tab", "j", "esc")

		assert.Equal(t, "treeview", readHoveredWorktree(harness.readFrame()))
	})

	t.Run("the strip left from the stop comes back on its chosen column", func(t *testing.T) {
		t.Parallel()

		harness := openGroupedWorktrees(t, buildMeasuredWorktrees()...)
		harness.press("f", "shift+tab", "esc", "f", "k")

		frame := harness.readFrame()
		assert.Contains(t, readSidebar(frame)[1], "↑ size")
		assert.NotContains(t, readBelowViews(frame)[0], "flat")
	})
}

// TestWorktreesScope covers the row of scopes narrowing which directories are listed.
func TestWorktreesScope(t *testing.T) {
	t.Parallel()

	occupied := buildMeasuredWorktrees()
	for _, row := range occupied {
		if agent, isAgent := row.(*agentv1.Agent); isAgent && agent.GetId() == "occ" {
			agent.Cwd = "/trees/archived"
		}
	}

	tests := []struct {
		name      string
		rows      []proto.Message
		press     []string
		want      []string
		wantTally string
	}{
		{
			name:      "the listing opens flat on every directory, the largest first",
			rows:      buildMeasuredWorktrees(),
			want:      []string{"cargo-fix", "reviewer", "treeview", "broken", "task/orphan"},
			wantTally: "5 · ",
		},
		{
			name:      "inactive leaves out the directory a live agent answers for",
			rows:      buildMeasuredWorktrees(),
			press:     []string{"F", "tab", "esc"},
			want:      []string{"cargo-fix", "reviewer", "broken", "task/orphan"},
			wantTally: "4 · ",
		},
		{
			name:      "inactive leaves out an archived agent's directory a live agent works in",
			rows:      occupied,
			press:     []string{"F", "tab", "esc"},
			want:      []string{"cargo-fix", "broken", "task/orphan"},
			wantTally: "3 · ",
		},
		{
			name:      "merged keeps the inactive directories the base holds whole",
			rows:      buildMeasuredWorktrees(),
			press:     []string{"F", "tab", "tab", "esc"},
			want:      []string{"cargo-fix"},
			wantTally: "1 · ",
		},
		{
			name:      "the row wraps back to every directory",
			rows:      buildMeasuredWorktrees(),
			press:     []string{"F", "tab", "tab", "tab", "esc"},
			want:      []string{"cargo-fix", "reviewer", "treeview", "broken", "task/orphan"},
			wantTally: "5 · ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openWorktrees(t, tt.rows...)
			moveToWorktree(t, harness, "reviewer")

			harness.press(tt.press...)

			frame := harness.readFrame()
			assert.Equal(t, tt.want, readListRows(frame))
			assert.True(t, strings.HasPrefix(readBelowViews(frame)[0], tt.wantTally), readBelowViews(frame)[0])
		})
	}

	t.Run("a directory both scopes list keeps the cursor", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t, buildMeasuredWorktrees()...)
		moveToWorktree(t, harness, "broken")

		harness.press("F", "tab", "esc")
		assert.Equal(t, "broken", readHoveredWorktree(harness.readFrame()))
	})

	t.Run("the scope holds while directories are selected", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t, buildMeasuredWorktrees()...)
		harness.press(append(widenListPresses, "v", "F", "?")...)

		frame := harness.readFrame()
		assert.NotEmpty(t, readHoveredRow(frame), "the list keeps the keys")
		assert.NotContains(t, readLegend(frame), "F kind")
	})

	t.Run("a jump to a directory the scope leaves out widens it to every directory", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t, buildMeasuredWorktrees()...)
		harness.press("F", "tab", "esc", "tab")
		moveToAgentRow(t, harness, "treeview")

		harness.press("space", "J", "w")

		frame := harness.readFrame()
		assert.Equal(t, worktreesScreen, harness.readScreen())
		assert.Equal(t, "treeview", readHoveredWorktree(frame))
		assert.True(t, strings.HasPrefix(readBelowViews(frame)[0], "5 · "), readBelowViews(frame)[0])
	})
}

const worktreesScreen = "the worktrees"

func openWorktrees(t *testing.T, rows ...proto.Message) *panelHarness {
	t.Helper()

	if len(rows) == 0 {
		rows = buildWorktrees()
	}

	harness := openAgents(t, rows...)
	harness.press("shift+tab", "esc")

	return harness
}

func openReadWorktree(t *testing.T) *panelHarness {
	t.Helper()

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

	return harness
}

func openGroupedWorktrees(t *testing.T, rows ...proto.Message) *panelHarness {
	t.Helper()

	harness := openWorktrees(t, rows...)
	harness.press("f", "shift+tab", "j", "esc")

	return harness
}

func buildWorktrees() []proto.Message {
	live := newAgent("live", "treeview", "w1")
	live.Harness = "claude"
	live.Cwd = "/trees/live"
	live.Alive = xptr.Ptr(true)
	live.State = agentv1.AgentState_AGENT_STATE_THINKING

	occupant := newAgent("occ", "review-607", "w1")
	occupant.Cwd = "/trees/live"
	occupant.Alive = xptr.Ptr(true)

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
		newWorktreeTask(live),
		newWorktreeTask(archived),
		newWorktreeTask(admin),
	}, buildWatchedWorktrees()...)
}

func buildWatchedWorktrees() []proto.Message {
	return []proto.Message{
		&worktreev1.WatchedWorktree{
			Dir:      "/trees/admin",
			RepoDir:  "/home/h/gadgets/admin",
			Branch:   "task/admin",
			Readable: true,
		},
		&worktreev1.WatchedWorktree{
			Dir:      "/trees/archived",
			RepoDir:  "/home/h/komrad",
			Branch:   "task/archived",
			Readable: true,
		},
		&worktreev1.WatchedWorktree{
			Dir:     "/trees/broken",
			RepoDir: "/var/folders/T/repo",
		},
		&worktreev1.WatchedWorktree{
			Dir:      "/trees/live",
			RepoDir:  "/home/h/komrad",
			Branch:   "task/live",
			Readable: true,
		},
		&worktreev1.WatchedWorktree{
			Dir:      "/trees/orphan",
			RepoDir:  "/home/h/komrad",
			Branch:   "task/orphan",
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
		agent.ArchivedAt = xptr.Ptr(formatStampAgo(4 * 24 * time.Hour))
	case "admin":
		agent.ArchivedAt = xptr.Ptr(formatStampAgo(12 * time.Hour))
	}
}

func measureWorktree(worktree *worktreev1.WatchedWorktree) {
	switch worktree.GetDir() {
	case "/trees/live", "/trees/archived":
		worktree.SizeBytes = xptr.Ptr[int64](49 * 1024 * 1024)
		worktree.ShareBytes = xptr.Ptr[int64](30 * 1024 * 1024)
		worktree.MeasuredAt = xptr.Ptr(formatStampAgo(time.Hour))
		worktree.Standing = &worktreev1.WorktreeStanding{ReadAt: formatStampAgo(time.Hour), Unlanded: 2}
	case "/trees/orphan":
		worktree.MeasuredAt = xptr.Ptr(formatStampAgo(time.Hour))
		worktree.SizeError = "permission denied"
		worktree.Standing = &worktreev1.WorktreeStanding{ReadAt: formatStampAgo(time.Hour), Error: "git cannot read the directory"}
		worktree.ModifiedAt = formatStampAgo(3 * time.Hour)
	case "/trees/broken":
		worktree.ModifiedAt = formatStampAgo(2 * 7 * 24 * time.Hour)
	case "/trees/admin":
		worktree.SizeBytes = xptr.Ptr[int64](1536 * 1024 * 1024)
		worktree.ShareBytes = xptr.Ptr[int64](1536 * 1024 * 1024)
		worktree.MeasuredAt = xptr.Ptr(formatStampAgo(time.Hour))
		worktree.Standing = &worktreev1.WorktreeStanding{ReadAt: formatStampAgo(time.Hour)}
	}
}

func newMeasuredWorktree(dir string, size int64) *worktreev1.WatchedWorktree {
	for _, row := range buildWatchedWorktrees() {
		worktree, _ := row.(*worktreev1.WatchedWorktree)
		if worktree.GetDir() == dir {
			worktree.SizeBytes = xptr.Ptr(size)
			worktree.ShareBytes = xptr.Ptr(size)
			worktree.MeasuredAt = xptr.Ptr(formatStampAgo(time.Minute))

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
	lines := xslice.Map(readStage(frame), strings.TrimSpace)

	return joinProse(lines)
}

// newWorktreeTask is the task an agent leads, its id the directory it works in and its slug
// the agent's name, put away when the agent is.
func newWorktreeTask(agent *agentv1.Agent) *taskv1.Task {
	agent.TaskId = xptr.Ptr(filepath.Base(agent.GetCwd()))

	return &taskv1.Task{
		Id:         agent.GetTaskId(),
		WorkflowId: agent.GetWorkflowId(),
		Slug:       agent.GetName(),
		Cwd:        xptr.Ptr(agent.GetCwd()),
		ArchivedAt: agent.ArchivedAt,
	}
}

// buildTaskWorktrees is two tasks' checkouts no agent answers for, beside a directory nothing does.
func buildTaskWorktrees() []proto.Message {
	return []proto.Message{
		newLiveWorkflow("w1", "komrad"),
		&taskv1.Task{Id: "t8", WorkflowId: "w1", Slug: "alpha"},
		&taskv1.Task{Id: "t9", WorkflowId: "w1", Slug: "beta"},
		&worktreev1.WatchedWorktree{Dir: "/trees/t8", RepoDir: "/home/h/komrad", Branch: "task/t8", Readable: true},
		&worktreev1.WatchedWorktree{Dir: "/trees/t9", RepoDir: "/home/h/komrad", Branch: "task/t9", Readable: true},
		&worktreev1.WatchedWorktree{Dir: "/trees/zz", RepoDir: "/home/h/komrad", Branch: "agent/zz", Readable: true},
	}
}
