package ui_test

import (
	"strings"
	"testing"
	"time"

	"komrad/internal/ui/paneltest"
	"komrad/lib/xslice"
	watchv1 "komrad/proto/gen/watch/v1"
	worktreev1 "komrad/proto/gen/worktree/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWatch covers the listing the panel keeps from its watch stream.
func TestWatch(t *testing.T) {
	t.Parallel()

	t.Run("a snapshot replaces everything the last stream built", func(t *testing.T) {
		t.Parallel()

		harness := paneltest.OpenAgents(t,
			paneltest.Workflow("w1", "api"),
			paneltest.Workflow("w2", "web"),
			newAgentAt("older", "w1", 1),
		)
		require.Equal(t, []string{"api", "└─ older", "web"}, paneltest.ListRows(harness.Frame()))

		harness.Watch(paneltest.Snapshot(
			paneltest.Workflow("w1", "api"),
			newAgentAt("two", "w1", 3),
			newAgentAt("older", "w1", 1),
		))

		assert.Equal(t, []string{"api", "├─ two", "└─ older"}, paneltest.ListRows(harness.Frame()))
	})

	t.Run("a task's tombstones take its writer and reviewer with them", func(t *testing.T) {
		t.Parallel()

		writer := paneltest.Agent("a1", "writer", "w1")
		harness := paneltest.OpenAgents(t,
			paneltest.Workflow("w1", "api"),
			writer,
			paneltest.TaskOf(writer),
			paneltest.Reviewer("a2", "reviewer", "a1"),
			paneltest.Agent("a3", "other", "w1"),
		)
		require.Len(t, paneltest.ListRows(harness.Frame()), 4)

		harness.Watch(
			paneltest.Deleted(watchv1.Resource_RESOURCE_AGENT, "a1"),
			paneltest.Deleted(watchv1.Resource_RESOURCE_AGENT, "a2"),
			paneltest.Deleted(watchv1.Resource_RESOURCE_TASK, "task-a1"),
		)

		assert.Equal(t, []string{"api", "└─ other"}, paneltest.ListRows(harness.Frame()))
	})

	t.Run("a put lands over the row it names and the cursor stays on it", func(t *testing.T) {
		t.Parallel()

		harness := paneltest.OpenAgents(t,
			paneltest.Workflow("w1", "api"),
			newAgentAt("one", "w1", 1),
			newAgentAt("two", "w1", 2),
		)
		harness.Press("j")
		require.Equal(t, "└─ one", paneltest.HoveredRow(harness.Frame()))

		renamed := newAgentAt("one", "w1", 1)
		renamed.Name = "renamed"
		harness.Watch(
			paneltest.Puts(renamed, newAgentAt("three", "w1", 3), paneltest.Workflow("w0", "aaa")),
			paneltest.Deleted(watchv1.Resource_RESOURCE_AGENT, "two"),
		)

		frame := harness.Frame()
		assert.Equal(t, []string{"aaa", "api", "├─ three", "└─ renamed"}, paneltest.ListRows(frame))
		assert.Equal(t, "└─ renamed", paneltest.HoveredRow(frame))
	})

	t.Run("a workflow renamed out of its place is filed again", func(t *testing.T) {
		t.Parallel()

		harness := paneltest.OpenAgents(t, paneltest.Workflow("w1", "api"), paneltest.Workflow("w2", "web"))

		harness.Watch(paneltest.Puts(paneltest.Workflow("w1", "zzz")))

		assert.Equal(t, []string{"web", "zzz"}, paneltest.ListRows(harness.Frame()))
	})

	t.Run("a workflow's tombstone takes it off the list", func(t *testing.T) {
		t.Parallel()

		harness := paneltest.OpenAgents(t, paneltest.Workflow("w0", "aaa"), paneltest.Workflow("w1", "api"))

		harness.Watch(paneltest.Deleted(watchv1.Resource_RESOURCE_WORKFLOW, "w0"))

		assert.Equal(t, []string{"api"}, paneltest.ListRows(harness.Frame()))
	})

	t.Run("a schedule's tombstone takes it off the schedules screen", func(t *testing.T) {
		t.Parallel()

		harness := openSchedules(t, buildSchedules()...)
		require.Len(t, readScheduleRows(harness.Frame()), 1)

		harness.Watch(paneltest.Deleted(watchv1.Resource_RESOURCE_SCHEDULE, "s1"))

		assert.Empty(t, readScheduleRows(harness.Frame()))
	})

	t.Run("unclaimed mail is marked until it expires by the panel's clock or is claimed", func(t *testing.T) {
		t.Parallel()

		expired := newUnreadMail("expired")
		expired.Id = "m2"
		expired.ExpiresAt = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
		harness := paneltest.OpenAgents(t,
			paneltest.Workflow("w1", "api"),
			newAgentAt("waiting", "w1", 2),
			newAgentAt("expired", "w1", 1),
			newUnreadMail("waiting"),
			expired,
		)

		frame := harness.Frame()
		assert.Contains(t, findRow(paneltest.ListRows(frame), "waiting"), mailGlyph)
		assert.NotContains(t, findRow(paneltest.ListRows(frame), "expired"), mailGlyph)

		harness.Watch(paneltest.Deleted(watchv1.Resource_RESOURCE_MAIL, "m1"))

		assert.NotContains(t, findRow(paneltest.ListRows(harness.Frame()), "waiting"), mailGlyph)
	})

	t.Run("a worktree's tombstone takes it off the worktrees screen", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		require.Contains(t, harness.Frame(), "task/orphan")

		harness.Watch(paneltest.Deleted(watchv1.Resource_RESOURCE_WORKTREE, findOrphanDir(t)))

		assert.NotContains(t, harness.Frame(), "task/orphan")
	})
}

const mailGlyph = ""

func buildDeletedWorktrees(dirs ...string) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Changes{Changes: &watchv1.Changes{Deleted: xslice.Map(dirs, func(dir string) *watchv1.Deleted {
		return &watchv1.Deleted{Resource: watchv1.Resource_RESOURCE_WORKTREE, Key: dir}
	})}}}
}

func findOrphanDir(t *testing.T) string {
	t.Helper()

	for _, row := range buildWatchedWorktrees() {
		worktree, isWorktree := row.(*worktreev1.WatchedWorktree)
		if isWorktree && strings.HasSuffix(worktree.GetBranch(), "orphan") {
			return worktree.GetDir()
		}
	}

	require.FailNow(t, "no orphaned worktree to drop")

	return ""
}

func findRow(rows []string, name string) string {
	for _, row := range rows {
		if strings.Contains(row, name) {
			return row
		}
	}

	return ""
}
