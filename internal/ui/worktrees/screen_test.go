package worktrees_test

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
	"komrad/internal/ui/paneltest"
	"komrad/internal/ui/screen"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	taskv1 "komrad/proto/gen/task/v1"
	"komrad/proto/gen/task/v1/taskv1connect"
	workflowv1 "komrad/proto/gen/workflow/v1"
	worktreev1 "komrad/proto/gen/worktree/v1"
	"komrad/proto/gen/worktree/v1/worktreev1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestWorktreesOpen covers the window and the title the screen opens with.
func TestWorktreesOpen(t *testing.T) {
	t.Parallel()

	t.Run("the panel keeps the whole window, a live agent's pane included", func(t *testing.T) {
		t.Parallel()

		rows := buildWorktrees()
		rows[1].(*agentv1.Agent).PaneId = "%7"
		harness := paneltest.OpenAgents(t, rows...)
		harness.Press("shift+tab")

		assert.True(t, harness.Tmux().IsZoomed())
	})

	t.Run("the title counts the directories and totals what has been measured", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		require.Contains(t, paneltest.BelowViews(harness.Frame())[0], "5 · —")

		harness.Watch(paneltest.Puts(
			newMeasuredWorktree("/trees/live", 49*1024*1024),
			newMeasuredWorktree("/trees/archived", 49*1024*1024),
			newMeasuredWorktree("/trees/admin", 1536*1024*1024),
		))

		assert.Contains(t, paneltest.BelowViews(harness.Frame())[0], "5 · 1.6G")
	})
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
			wantRendered: `Delete task "reviewer"?`,
		},
		{
			name:         "a directory nothing answers for is offered the delete on its own",
			row:          "broken",
			press:        []string{"space", "a", "d"},
			wantRendered: "Delete the worktree of broken? Komrad deletes the directory, which no agent uses",
		},
		{
			name:         "a selection of two directories nothing answers for counts them",
			row:          "task/orphan",
			press:        []string{"v", "j", "v", "space", "a", "d"},
			wantRendered: "Delete 2 worktrees? Komrad deletes 2 directories that no agent uses",
		},
		{
			name:         "deleting unused worktrees gathers only the directories no task answers for",
			row:          "treeview",
			press:        []string{"space", "c"},
			wantRendered: "Delete 2 worktrees?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openGroupedWorktrees(t)
			moveToWorktree(t, harness, tt.row)

			harness.Press(tt.press...)

			frame := harness.Frame()
			prose := paneltest.StageProse(frame) + " " + paneltest.SidebarProse(frame)
			assert.Contains(t, prose, tt.wantRendered)
			if tt.wantAbsent != "" {
				assert.NotContains(t, prose, tt.wantAbsent)
			}
		})
	}

	t.Run("deleting unused worktrees is gray and asks nothing when only live directories are listed", func(t *testing.T) {
		t.Parallel()

		live := paneltest.Agent("live", "treeview", "w1")
		live.Cwd = "/trees/live"
		live.Alive = xptr.Ptr(true)
		harness := openWorktrees(t,
			paneltest.Workflow("w1", "komrad"),
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

		harness.Press("space")
		assert.Regexp(t, readOffEntry("delete unused worktrees", "c"), harness.RawFrame())

		harness.Press("c", "y")

		assert.NotContains(t, paneltest.StageProse(harness.Frame()), "Delete")
		harness.AssertSentTo(worktreev1connect.WorktreeServiceDeleteWorktreesProcedure)
	})

	t.Run("chosen task checkouts", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name        string
			press       []string
			wantRefusal string
			wantSent    []proto.Message
		}{
			{
				name:        "are kept when nothing is checked",
				press:       []string{"x", "enter"},
				wantRefusal: "check what to delete, or go back",
			},
			{
				name:     "are deleted once something is checked after a refused confirm",
				press:    []string{"x", "tab", "y", "x", "enter"},
				wantSent: []proto.Message{&taskv1.DeleteTasksRequest{TaskIds: []string{"t8", "t9"}}},
			},
			{
				name:     "are deleted once something is checked after a confirm refused on enter",
				press:    []string{"x", "tab", "enter", "x", "enter"},
				wantSent: []proto.Message{&taskv1.DeleteTasksRequest{TaskIds: []string{"t8", "t9"}}},
			},
			{
				name:     "are deleted with their worktrees alone by default",
				press:    []string{"enter"},
				wantSent: []proto.Message{&taskv1.DeleteTasksRequest{TaskIds: []string{"t8", "t9"}}},
			},
			{
				name:     "are deleted with their records once records are checked",
				press:    []string{"down", "x", "enter"},
				wantSent: []proto.Message{&taskv1.DeleteTasksRequest{TaskIds: []string{"t8", "t9"}, Records: true}},
			},
			{
				name:     "are deleted with their records when only the records are checked",
				press:    []string{"x", "down", "x", "enter"},
				wantSent: []proto.Message{&taskv1.DeleteTasksRequest{TaskIds: []string{"t8", "t9"}, Records: true}},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				harness := openWorktrees(t, buildTaskWorktrees()...)
				moveToWorktree(t, harness, "alpha")

				harness.Press(append([]string{"v", "j", "v", "space", "a", "d"}, tt.press...)...)

				if tt.wantRefusal != "" {
					assert.Contains(t, harness.Frame(), tt.wantRefusal)
				}
				harness.AssertSentTo(taskv1connect.TaskServiceDeleteTasksProcedure, tt.wantSent...)
			})
		}
	})

	t.Run("a choice mixing a task checkout with the rest asks once and deletes each its own way", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t, buildTaskWorktrees()...)
		moveToWorktree(t, harness, "beta")

		harness.Press("v", "j", "v", "space", "a", "d")

		assert.Contains(t, harness.Frame(), "Delete 2 worktrees?")

		harness.Press("tab", "y")

		harness.AssertSentTo(taskv1connect.TaskServiceDeleteTasksProcedure, &taskv1.DeleteTasksRequest{TaskIds: []string{"t9"}})
		harness.AssertSentTo(worktreev1connect.WorktreeServiceDeleteWorktreesProcedure, &worktreev1.DeleteWorktreesRequest{Dirs: []string{"/trees/zz"}})
	})
}

// TestWorktreesDeleteStanding covers what a delete confirm says the worktrees it takes still hold.
func TestWorktreesDeleteStanding(t *testing.T) {
	t.Parallel()

	archived := &worktreev1.Worktree{
		Dir:      "/trees/archived",
		Branch:   "task/archived",
		Readable: true,
	}
	orphan := &worktreev1.Worktree{
		Dir:      "/trees/orphan",
		Branch:   "task/orphan",
		Readable: true,
	}
	dirty := &worktreev1.GetWorktreeResponse{
		Worktree:  archived,
		Modified:  []string{"a.go"},
		Untracked: []string{"b.txt", "c.txt"},
		BaseRef:   "origin/main",
		Pushed:    true,
		Unpushed:  2,
	}
	clean := &worktreev1.GetWorktreeResponse{
		Worktree: archived,
		BaseRef:  "origin/main",
		Pushed:   true,
	}
	baseless := &worktreev1.GetWorktreeResponse{
		Worktree:  orphan,
		Untracked: []string{"notes.md"},
		BaseError: "the workflow names no base branch",
	}
	crowded := &worktreev1.GetWorktreeResponse{
		Worktree: orphan,
		Modified: buildFileNames(12),
		BaseRef:  "origin/main",
		Ahead:    3,
	}
	detached := &worktreev1.GetWorktreeResponse{
		Worktree: &worktreev1.Worktree{
			Dir:      "/trees/orphan",
			Readable: true,
		},
		BaseRef: "origin/main",
		Ahead:   2,
	}

	tests := []struct {
		name         string
		row          string
		press        []string
		answer       paneltest.AnswerFunc
		wantRendered []string
		wantAbsent   []string
	}{
		{
			name:         "a task's delete counts what is uncommitted and unpushed and names the files",
			row:          "reviewer",
			press:        []string{"space", "a", "d"},
			answer:       paneltest.AnswerWith(dirty),
			wantRendered: []string{"The worktree has 3 uncommitted files and 2 unpushed commits. Deleting the worktree loses the uncommitted files.", "a.go b.txt c.txt"},
		},
		{
			name:         "a clean worktree is said to be clean",
			row:          "reviewer",
			press:        []string{"space", "a", "d"},
			answer:       paneltest.AnswerWith(clean),
			wantRendered: []string{"The worktree has nothing uncommitted or unpushed."},
		},
		{
			name:         "a branch never pushed with no base counts its files and not its commits",
			row:          "task/orphan",
			press:        []string{"space", "a", "d"},
			answer:       paneltest.AnswerWith(baseless),
			wantRendered: []string{"The worktree has 1 uncommitted file. Deleting the worktree loses the uncommitted files. Without a base, Komrad cannot count the unpushed commits of the worktree.", "notes.md"},
		},
		{
			name:         "a branch never pushed counts its commits since the base and the files past ten are counted",
			row:          "task/orphan",
			press:        []string{"space", "a", "d"},
			answer:       paneltest.AnswerWith(crowded),
			wantRendered: []string{"The worktree has 12 uncommitted files and 3 unpushed commits.", "f9.go and 2 more"},
			wantAbsent:   []string{"You lose any uncommitted changes"},
		},
		{
			name:         "a worktree git refuses says why it cannot be counted",
			row:          "reviewer",
			press:        []string{"space", "a", "d"},
			answer:       paneltest.RefuseWith(connect.CodeInternal, "git exploded"),
			wantRendered: []string{"Komrad could not read the worktree (git exploded), so it cannot say what is uncommitted or unpushed there."},
		},
		{
			name:         "several worktrees are counted together without naming files",
			row:          "task/orphan",
			press:        []string{"v", "k", "v", "space", "a", "d"},
			answer:       paneltest.AnswerWith(dirty),
			wantRendered: []string{"The worktrees have 6 uncommitted files and 4 unpushed commits. Deleting the worktrees loses the uncommitted files."},
			wantAbsent:   []string{"a.go"},
		},
		{
			name:         "a detached head counts its commits since the base",
			row:          "task/orphan",
			press:        []string{"space", "a", "d"},
			answer:       paneltest.AnswerWith(detached),
			wantRendered: []string{"The worktree has 2 unpushed commits."},
		},
		{
			name:         "several worktrees of which one is refused count only those read and name their files",
			row:          "task/orphan",
			press:        []string{"v", "k", "v", "space", "a", "d"},
			answer:       answerForDir("/trees/orphan", paneltest.AnswerWith(dirty), paneltest.RefuseWith(connect.CodeInternal, "git exploded")),
			wantRendered: []string{"The 1 worktree Komrad read has 3 uncommitted files and 2 unpushed commits. Deleting the worktrees loses the uncommitted files. Komrad could not read 1 worktree, so it cannot say what is uncommitted or unpushed there.", "a.go b.txt c.txt"},
		},
		{
			name:         "several worktrees of which one is gone are spoken of as one",
			row:          "task/orphan",
			press:        []string{"v", "k", "v", "space", "a", "d"},
			answer:       answerForDir("/trees/orphan", paneltest.AnswerWith(dirty), paneltest.RefuseWith(connect.CodeNotFound, "gone")),
			wantRendered: []string{"The worktree has 3 uncommitted files and 2 unpushed commits. Deleting the worktree loses the uncommitted files.", "a.go b.txt c.txt"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openGroupedWorktrees(t)
			harness.Answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, tt.answer)
			moveToWorktree(t, harness, tt.row)

			harness.Press(tt.press...)

			prose := paneltest.StageProse(harness.Frame())
			for _, want := range tt.wantRendered {
				assert.Contains(t, prose, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, prose, absent)
			}
		})
	}

	t.Run("a confirm opens while git is still reading and counts once it answers", func(t *testing.T) {
		t.Parallel()

		harness := openGroupedWorktrees(t)
		harness.Answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, paneltest.AnswerWith(dirty))
		harness.Hold(worktreev1connect.WorktreeServiceGetWorktreeProcedure)
		moveToWorktree(t, harness, "reviewer")

		harness.Press("space", "a", "d")

		assert.Contains(t, paneltest.StageProse(harness.Frame()), "Komrad is still reading the worktree, so it cannot yet say what is uncommitted or unpushed there.")

		harness.Release(worktreev1connect.WorktreeServiceGetWorktreeProcedure)

		assert.Contains(t, paneltest.StageProse(harness.Frame()), "The worktree has 3 uncommitted files and 2 unpushed commits.")
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
			name:  "a repository header offers only deleting unused worktrees",
			row:   "/home/h/komrad",
			label: "delete unused worktrees",
			key:   "c",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openGroupedWorktrees(t)
			moveToWorktree(t, harness, tt.row)

			harness.Press(append(tt.press, "space", "a")...)

			entry := regexp.MustCompile(`(?m)› ` + regexp.QuoteMeta(tt.label) + ` +` + tt.key + `\s*$`)
			assert.Regexp(t, entry, harness.Frame())
		})
	}
}

// TestWorktreesDeleteAnswer covers answering the prompt deleting unused worktrees opens.
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
				harness.Refuse(worktreev1connect.WorktreeServiceDeleteWorktreesProcedure, tt.refusal)
			}
			harness.Press(paneltest.WidenListPresses...)

			harness.Press("space", "c", tt.answer)
			if len(tt.gone) > 0 {
				harness.Watch(buildDeletedWorktrees(tt.gone...))
			}

			harness.AssertSentTo(worktreev1connect.WorktreeServiceDeleteWorktreesProcedure, tt.wantSent...)
			frame := harness.Frame()
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
		harness.Press(paneltest.WidenListPresses...)

		frame := harness.Frame()
		assert.Equal(t, "/var/folders/T/repo", paneltest.RecordField(frame, "repository"))
		assert.Equal(t, "none", paneltest.RecordField(frame, "task"))
		assert.Empty(t, paneltest.RecordField(frame, "agents"))
		assert.Empty(t, paneltest.RecordField(frame, "branch"))
	})

	t.Run("the listing names the task and the agents working in its directory", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		moveToWorktree(t, harness, "reviewer")
		harness.Press(paneltest.WidenListPresses...)

		frame := harness.Frame()
		assert.Equal(t, "reviewer", paneltest.RecordField(frame, "name"))
		assert.Equal(t, "—", paneltest.RecordField(frame, "size"))
		assert.Equal(t, "task/archived", paneltest.RecordField(frame, "branch"))
		assert.Equal(t, "komrad", paneltest.RecordField(frame, "workflow"))
		assert.Equal(t, "reviewer", paneltest.RecordField(frame, "task"))
		assert.Equal(t, "reviewer", paneltest.RecordField(frame, "agents"))
		assert.Empty(t, paneltest.RecordField(frame, "base"))
	})

	t.Run("a directory named after its task names the agents provisioned for the task and those working in it", func(t *testing.T) {
		t.Parallel()

		writer := paneltest.Agent("writer-id", "writer", "w1")
		writer.Cwd = "/home/h/komrad"
		task := paneltest.TaskOf(writer)
		reviewer := paneltest.Agent("reviewer-id", "reviewer", "w1")
		reviewer.Cwd = "/trees/" + task.GetId()
		reviewer.TaskId = writer.TaskId
		reviewer.Role = agentv1.AgentRole_AGENT_ROLE_REVIEWER
		adopter := paneltest.Agent("adopter-id", "adopter", "w1")
		adopter.Cwd = writer.GetCwd()
		adopter.TaskId = writer.TaskId
		adopter.ProvisionDisabled = true

		harness := openWorktrees(t,
			paneltest.Workflow("w1", "komrad"),
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
		harness.Press(paneltest.WidenListPresses...)
		harness.Press("pick:record")

		assert.Equal(t, "writer, reviewer", paneltest.RecordField(harness.Frame(), "agents"))
	})

	t.Run("a live writer is named before a newer archived reviewer", func(t *testing.T) {
		t.Parallel()

		writer := paneltest.Agent("writer-id", "writer", "w1")
		writer.CreatedAt = timestamppb.New(time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC))
		task := paneltest.TaskOf(writer)
		writer.Cwd = "/trees/" + task.GetId()
		reviewer := newArchivedAgent("reviewer-id", "reviewer", "w1")
		reviewer.CreatedAt = timestamppb.New(time.Date(2026, 9, 20, 13, 0, 0, 0, time.UTC))
		reviewer.Cwd = writer.GetCwd()
		reviewer.TaskId = writer.TaskId
		reviewer.Role = agentv1.AgentRole_AGENT_ROLE_REVIEWER

		harness := openWorktrees(t,
			paneltest.Workflow("w1", "komrad"),
			writer,
			reviewer,
			task,
			&worktreev1.WatchedWorktree{
				Dir:      writer.GetCwd(),
				RepoDir:  "/home/h/komrad",
				Branch:   "task/" + task.GetId(),
				Readable: true,
			},
		)
		moveToWorktree(t, harness, task.GetSlug())
		harness.Press(paneltest.WidenListPresses...)
		harness.Press("pick:record")

		assert.Equal(t, "writer, reviewer", paneltest.RecordField(harness.Frame(), "agents"))
	})

	t.Run("a task's checkout no agent answers for names its task", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t,
			paneltest.Workflow("w1", "komrad"),
			&taskv1.Task{
				Id:         "t9",
				WorkflowId: "w1",
				Slug:       "fix-login",
			},
			&worktreev1.WatchedWorktree{
				Dir:      "/trees/t9",
				RepoDir:  "/home/h/komrad",
				Branch:   "task/t9",
				Readable: true,
			},
		)
		moveToWorktree(t, harness, "fix-login")
		harness.Press(paneltest.WidenListPresses...)

		frame := harness.Frame()
		assert.Equal(t, "fix-login", paneltest.RecordField(frame, "task"))
		assert.Empty(t, paneltest.RecordField(frame, "agents"))
	})

	t.Run("the read of the directory adds where the branch stands and what it changed", func(t *testing.T) {
		t.Parallel()

		harness := openReadWorktree(t)
		harness.Press(paneltest.WidenListPresses...)

		frame := harness.Frame()
		assert.Equal(t, "origin/main ↑2 ↓12", paneltest.RecordField(frame, "base"))
		assert.Equal(t, "1 modified, 2 untracked", paneltest.RecordField(frame, "changes"))
		raw := harness.RawFrame()
		assert.Contains(t, raw, paint.Panel.Style(paint.ToneSuccess).Render("↑2"))
		assert.Contains(t, raw, paint.Panel.Style(paint.ToneWarning).Render("↓12"))
	})

	t.Run("a field copies its value without its tail", func(t *testing.T) {
		t.Parallel()

		harness := openReadWorktree(t)
		harness.Press("pick:record", "/", "m", "a", "i", "n", "enter", "y")

		assert.Equal(t, []string{"origin/main"}, harness.Copied())
	})

	t.Run("a search lights its match in the value", func(t *testing.T) {
		t.Parallel()

		harness := openReadWorktree(t)
		harness.Press(append([]string{"pick:record", "/", "m", "a", "i", "n", "enter"}, paneltest.WidenListPresses...)...)

		line := findRawLine(harness.RawFrame(), regexp.MustCompile(`base\s+origin/main`))
		assert.Contains(t, line, paint.Panel.Match().Render("main"))
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
				MeasuredAt: stampAgo(5 * time.Minute),
				SizeError:  "permission denied",
				ModifiedAt: stampAgo(90 * time.Minute),
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
				MeasuredAt: stampAgo(90 * time.Minute),
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
			rows[3].(*agentv1.Agent).ArchivedAt = stampAgo(4 * 24 * time.Hour)
			harness := openWorktrees(t, rows...)
			harness.Watch(paneltest.Puts(tt.worktree))
			moveToWorktree(t, harness, tt.row)
			harness.Press(paneltest.WidenListPresses...)

			frame := harness.Frame()
			assert.Equal(t, tt.wantSize, paneltest.RecordField(frame, "size"))
			assert.Equal(t, tt.wantLastActive, paneltest.RecordField(frame, "last active"))
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
				occupant := paneltest.Agent(fmt.Sprintf("occ%d", i), name, "w1")
				occupant.Cwd = tt.dir
				occupant.CreatedAt = timestamppb.New(time.Date(2026, 9, 20, 10+i, 0, 0, 0, time.UTC))
				rows = append(rows, occupant)
			}
			rows = append(rows, buildWatchedWorktrees()...)
			harness := openWorktrees(t, rows...)
			moveToWorktree(t, harness, tt.row)
			harness.Press(paneltest.WidenListPresses...)
			harness.Press("pick:record")

			assert.Equal(t, tt.want, paneltest.RecordField(harness.Frame(), "agents"))
		})
	}
}

// TestWorktreesDocument covers the document rendered beside the list.
func TestWorktreesDocument(t *testing.T) {
	t.Parallel()

	t.Run("a read git refused is rendered rather than asked again", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		harness.Refuse(worktreev1connect.WorktreeServiceGetWorktreeProcedure, connect.NewError(connect.CodeInternal, errors.New("git exploded")))
		moveToWorktree(t, harness, "task/orphan")
		harness.Advance(time.Second)
		asked := len(harness.Sent(worktreev1connect.WorktreeServiceGetWorktreeProcedure))

		harness.Watch(paneltest.Puts(newMeasuredWorktree("/trees/admin", 1024)))

		assert.Contains(t, paneltest.StageText(harness.Frame()), "git exploded")
		assert.Len(t, harness.Sent(worktreev1connect.WorktreeServiceGetWorktreeProcedure), asked)
	})

	t.Run("a reread refused keeps the last answer beside the refusal", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		harness.Answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, paneltest.AnswerWith(&worktreev1.GetWorktreeResponse{
			Worktree: &worktreev1.Worktree{Dir: "/trees/live"},
			BaseRef:  "origin/main",
		}))
		harness.Advance(time.Second)
		harness.Refuse(worktreev1connect.WorktreeServiceGetWorktreeProcedure, connect.NewError(connect.CodeInternal, errors.New("git exploded")))

		harness.Press("j", "k")
		harness.Advance(time.Second)

		document := paneltest.StageText(harness.Frame())
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
		harness.Answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, paneltest.AnswerWith(&worktreev1.GetWorktreeResponse{
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
		harness.Advance(time.Second)

		document := paneltest.StageText(harness.Frame())
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
		harness.AnswerLastScreens(map[string]string{"archived": "⏺ the reviewer approved"})

		moveToWorktree(t, harness, "reviewer")
		harness.Advance(time.Second)

		document := paneltest.StageText(harness.Frame())
		assert.Contains(t, document, "saved screen")
		assert.Contains(t, document, "the reviewer approved")
	})

	t.Run("a worktree with no owner has no saved screen", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		harness.AnswerLastScreens(nil)

		moveToWorktree(t, harness, "task/orphan")
		harness.Advance(time.Second)

		assert.NotContains(t, paneltest.StageText(harness.Frame()), "saved screen")
	})
}

// TestWorktreesInspection covers when the directory under the cursor is read.
func TestWorktreesInspection(t *testing.T) {
	t.Parallel()

	t.Run("the cursor coming to rest reads the directory it rests on, not the ones it passed", func(t *testing.T) {
		t.Parallel()

		harness := openGroupedWorktrees(t)

		harness.Press("g", "g", "j", "j", "j")
		harness.AssertSentTo(worktreev1connect.WorktreeServiceGetWorktreeProcedure)
		harness.Advance(time.Second)

		harness.AssertSentTo(worktreev1connect.WorktreeServiceGetWorktreeProcedure, &worktreev1.GetWorktreeRequest{Dir: "/trees/archived"})
	})

	t.Run("a listing that moves leaves the cursor on its row", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		moveToWorktree(t, harness, "task/orphan")

		harness.Watch(paneltest.Snapshot(append(buildWorktrees()[:5], buildWatchedWorktrees()[1:]...)...))

		assert.Equal(t, "task/orphan", readHoveredWorktree(harness.Frame()))
	})

	t.Run("a directory whose read is on its way is not asked for again", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		harness.Hold(worktreev1connect.WorktreeServiceGetWorktreeProcedure)
		moveToWorktree(t, harness, "reviewer")
		harness.Advance(time.Second)

		harness.Press("j", "k")
		harness.Advance(time.Second)

		assert.Len(t, readWorktreeReadsOf(harness, "/trees/archived"), 1)
	})

	t.Run("a row read again shows its last answer until the new one lands", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		harness.Answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, paneltest.AnswerWith(&worktreev1.GetWorktreeResponse{
			Worktree: &worktreev1.Worktree{
				Dir:      "/trees/archived",
				Readable: true,
			},
			BaseRef: "origin/main",
			Ahead:   2,
		}))
		moveToWorktree(t, harness, "reviewer")
		harness.Advance(time.Second)
		harness.Hold(worktreev1connect.WorktreeServiceGetWorktreeProcedure)

		harness.Press("j", "k")
		harness.Advance(time.Second)

		frame := harness.Frame()
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

			harness.Press(tt.press...)

			frame := harness.Frame()
			assert.Equal(t, tt.want, paneltest.ListRows(frame))
			assert.Equal(t, "treeview", readHoveredWorktree(frame), "the cursor keeps its row")
		})
	}

	t.Run("the arrow turns under the cursor and the list takes the keys back", func(t *testing.T) {
		t.Parallel()

		harness := openGroupedWorktrees(t, buildMeasuredWorktrees()...)
		harness.Press("f", "tab", "j", "f", "k")

		frame := harness.Frame()
		assert.Contains(t, paneltest.Sidebar(frame)[1], "↑ last-accessed", "the focus key is not the way back")
		assert.Equal(t, "treeview", paneltest.RecordField(frame, "name"), "the cursor follows its row as the arrow turns")

		harness.Press("esc", "j")
		assert.Equal(t, "/var/folders/T/repo", readHoveredWorktree(harness.Frame()))
	})

	t.Run("the stop flips the headings back and escape keeps what it chose", func(t *testing.T) {
		t.Parallel()

		harness := openGroupedWorktrees(t, buildMeasuredWorktrees()...)
		harness.Press("f", "tab", "tab", "j")
		require.Contains(t, paneltest.BelowViews(harness.Frame())[0], "flat")

		harness.Press("k")
		frame := harness.Frame()
		assert.NotContains(t, paneltest.BelowViews(frame)[0], "flat")
		assert.Equal(t, "treeview", paneltest.RecordField(frame, "name"))

		harness.Press("j", "esc")
		assert.Contains(t, paneltest.BelowViews(harness.Frame())[0], "flat")
	})

	t.Run("a cursor on a heading lands on a row once the headings go", func(t *testing.T) {
		t.Parallel()

		harness := openGroupedWorktrees(t, buildMeasuredWorktrees()...)
		harness.Press("k", "k")
		require.Equal(t, "/home/h/komrad", readHoveredWorktree(harness.Frame()))

		harness.Press("f", "shift+tab", "j", "esc")

		assert.Equal(t, "treeview", readHoveredWorktree(harness.Frame()))
	})

	t.Run("the strip left from the stop comes back on its chosen column", func(t *testing.T) {
		t.Parallel()

		harness := openGroupedWorktrees(t, buildMeasuredWorktrees()...)
		harness.Press("f", "shift+tab", "esc", "f", "k")

		frame := harness.Frame()
		assert.Contains(t, paneltest.Sidebar(frame)[1], "↑ size")
		assert.NotContains(t, paneltest.BelowViews(frame)[0], "flat")
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

			harness.Press(tt.press...)

			frame := harness.Frame()
			assert.Equal(t, tt.want, paneltest.ListRows(frame))
			assert.True(t, strings.HasPrefix(paneltest.BelowViews(frame)[0], tt.wantTally), paneltest.BelowViews(frame)[0])
		})
	}

	t.Run("a directory both scopes list keeps the cursor", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t, buildMeasuredWorktrees()...)
		moveToWorktree(t, harness, "broken")

		harness.Press("F", "tab", "esc")
		assert.Equal(t, "broken", readHoveredWorktree(harness.Frame()))
	})

	t.Run("the scope holds while directories are selected", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t, buildMeasuredWorktrees()...)
		harness.Press(append(paneltest.WidenListPresses, "v", "F", "?")...)

		frame := harness.Frame()
		assert.NotEmpty(t, paneltest.HoveredRow(frame), "the list keeps the keys")
		assert.NotContains(t, paneltest.Legend(frame), "F kind")
	})

	t.Run("a jump to a directory the scope leaves out widens it to every directory", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t, buildMeasuredWorktrees()...)
		harness.Press("F", "tab", "esc", "tab")
		harness.MoveToRow("treeview")

		harness.Press("space", "J", "w")

		frame := harness.Frame()
		assert.Equal(t, screen.Worktrees, paneltest.Screen(harness.Frame()))
		assert.Equal(t, "treeview", readHoveredWorktree(frame))
		assert.True(t, strings.HasPrefix(paneltest.BelowViews(frame)[0], "5 · "), paneltest.BelowViews(frame)[0])
	})
}

func openWorktrees(t *testing.T, rows ...proto.Message) *paneltest.Panel {
	t.Helper()

	if len(rows) == 0 {
		rows = buildWorktrees()
	}

	harness := paneltest.OpenAgents(t, rows...)
	harness.Press("shift+tab", "esc")

	return harness
}

func openReadWorktree(t *testing.T) *paneltest.Panel {
	t.Helper()

	harness := openWorktrees(t)
	harness.Answer(worktreev1connect.WorktreeServiceGetWorktreeProcedure, paneltest.AnswerWith(&worktreev1.GetWorktreeResponse{
		Worktree:  &worktreev1.Worktree{Dir: "/trees/archived"},
		BaseRef:   "origin/main",
		Ahead:     2,
		Behind:    12,
		Modified:  []string{"a.go"},
		Untracked: []string{"b.txt", "c.txt"},
	}))
	moveToWorktree(t, harness, "reviewer")
	harness.Advance(time.Second)

	return harness
}

func openGroupedWorktrees(t *testing.T, rows ...proto.Message) *paneltest.Panel {
	t.Helper()

	harness := openWorktrees(t, rows...)
	harness.Press("f", "shift+tab", "j", "esc")

	return harness
}

func buildWorktrees() []proto.Message {
	live := paneltest.Agent("live", "treeview", "w1")
	live.Harness = "claude"
	live.Cwd = "/trees/live"
	live.Alive = xptr.Ptr(true)
	live.State = agentv1.AgentState_AGENT_STATE_THINKING
	live.Role = agentv1.AgentRole_AGENT_ROLE_WRITER

	occupant := paneltest.Agent("occ", "review-607", "w1")
	occupant.Cwd = "/trees/live"
	occupant.Alive = xptr.Ptr(true)

	archived := newArchivedAgent("archived", "reviewer", "w1")
	archived.Harness = "claude"
	archived.Cwd = "/trees/archived"
	archived.Role = agentv1.AgentRole_AGENT_ROLE_WRITER

	admin := newArchivedAgent("admin", "cargo-fix", "w1")
	admin.Cwd = "/trees/admin"
	admin.Role = agentv1.AgentRole_AGENT_ROLE_WRITER

	return append([]proto.Message{
		paneltest.Workflow("w1", "komrad"),
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
		agent.StateChangedAt = stampAgo(5 * time.Minute)
	case "archived":
		agent.ArchivedAt = stampAgo(4 * 24 * time.Hour)
	case "admin":
		agent.ArchivedAt = stampAgo(12 * time.Hour)
	}
}

func measureWorktree(worktree *worktreev1.WatchedWorktree) {
	switch worktree.GetDir() {
	case "/trees/live", "/trees/archived":
		worktree.SizeBytes = xptr.Ptr[int64](49 * 1024 * 1024)
		worktree.ShareBytes = xptr.Ptr[int64](30 * 1024 * 1024)
		worktree.MeasuredAt = stampAgo(time.Hour)
		worktree.Standing = &worktreev1.WorktreeStanding{ReadAt: stampAgo(time.Hour), Unlanded: 2}
	case "/trees/orphan":
		worktree.MeasuredAt = stampAgo(time.Hour)
		worktree.SizeError = "permission denied"
		worktree.Standing = &worktreev1.WorktreeStanding{ReadAt: stampAgo(time.Hour), Error: "git cannot read the directory"}
		worktree.ModifiedAt = stampAgo(3 * time.Hour)
	case "/trees/broken":
		worktree.ModifiedAt = stampAgo(2 * 7 * 24 * time.Hour)
	case "/trees/admin":
		worktree.SizeBytes = xptr.Ptr[int64](1536 * 1024 * 1024)
		worktree.ShareBytes = xptr.Ptr[int64](1536 * 1024 * 1024)
		worktree.MeasuredAt = stampAgo(time.Hour)
		worktree.Standing = &worktreev1.WorktreeStanding{ReadAt: stampAgo(time.Hour)}
	}
}

func newMeasuredWorktree(dir string, size int64) *worktreev1.WatchedWorktree {
	for _, row := range buildWatchedWorktrees() {
		worktree, _ := row.(*worktreev1.WatchedWorktree)
		if worktree.GetDir() == dir {
			worktree.SizeBytes = xptr.Ptr(size)
			worktree.ShareBytes = xptr.Ptr(size)
			worktree.MeasuredAt = stampAgo(time.Minute)

			return worktree
		}
	}

	return nil
}

func moveToWorktree(t *testing.T, harness *paneltest.Panel, name string) {
	t.Helper()

	harness.Press("g", "g")
	for range 10 {
		if readHoveredWorktree(harness.Frame()) == name {
			return
		}
		harness.Press("j")
	}

	require.Equal(t, name, readHoveredWorktree(harness.Frame()))
}

func readWorktreeReadsOf(harness *paneltest.Panel, dir string) []proto.Message {
	return slices.DeleteFunc(harness.Sent(worktreev1connect.WorktreeServiceGetWorktreeProcedure), func(sent proto.Message) bool {
		read, _ := sent.(*worktreev1.GetWorktreeRequest)

		return read.GetDir() != dir
	})
}

func readHoveredWorktree(frame string) string {
	return strings.TrimSpace(paneltest.HoveredRow(frame))
}

func stampAgo(ago time.Duration) *timestamppb.Timestamp {
	return timestamppb.New(time.Now().Add(-ago))
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
		&workflowv1.Workflow{
			Id:       "w1",
			Name:     "komrad",
			Worktree: &workflowv1.WorktreeConfig{Enabled: true},
		},
		&taskv1.Task{
			Id:         "t8",
			WorkflowId: "w1",
			Slug:       "alpha",
			Cwd:        xptr.Ptr("/trees/t8"),
		},
		&taskv1.Task{
			Id:         "t9",
			WorkflowId: "w1",
			Slug:       "beta",
			Cwd:        xptr.Ptr("/trees/t9"),
		},
		&worktreev1.WatchedWorktree{
			Dir:      "/trees/t8",
			RepoDir:  "/home/h/komrad",
			Branch:   "task/t8",
			Readable: true,
		},
		&worktreev1.WatchedWorktree{
			Dir:      "/trees/t9",
			RepoDir:  "/home/h/komrad",
			Branch:   "task/t9",
			Readable: true,
		},
		&worktreev1.WatchedWorktree{
			Dir:      "/trees/zz",
			RepoDir:  "/home/h/komrad",
			Branch:   "agent/zz",
			Readable: true,
		},
	}
}

func buildFileNames(count int) []string {
	names := make([]string, count)
	for i := range names {
		names[i] = fmt.Sprintf("f%d.go", i)
	}

	return names
}

func answerForDir(dir string, answer, rest paneltest.AnswerFunc) paneltest.AnswerFunc {
	return func(req connect.AnyRequest) (connect.AnyResponse, error) {
		if read, _ := req.Any().(*worktreev1.GetWorktreeRequest); read.GetDir() == dir {
			return answer(req)
		}

		return rest(req)
	}
}
