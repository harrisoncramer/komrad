package ui_test

import (
	"strings"
	"testing"
	"time"

	"komrad/lib/xptr"
	watchv1 "komrad/proto/gen/watch/v1"
	worktreev1 "komrad/proto/gen/worktree/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const mailGlyph = ""

// TestWatch covers the listing the panel keeps from its watch stream.
func TestWatch(t *testing.T) {
	t.Parallel()

	t.Run("a snapshot replaces everything the last stream built", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t,
			newLiveWorkflow("w1", "api"),
			newLiveWorkflow("w2", "web"),
			newAgentAt("older", "w1", 1),
			newMark("a", "older"),
		)
		require.Equal(t, []string{"api", "  older", "web"}, readListRows(harness.readFrame()))

		harness.watch(buildSnapshot(
			newLiveWorkflow("w1", "api"),
			newAgentAt("two", "w1", 3),
			newAgentAt("older", "w1", 1),
		))
		harness.press("k", "'", "a")

		frame := harness.readFrame()
		assert.Equal(t, []string{"api", "  two", "  older"}, readListRows(frame))
		assert.Equal(t, "  two", readHoveredRow(frame), "the mark went with the stream that carried it")
	})

	t.Run("a mark for an agent not yet listed is worn once the agent arrives", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, newLiveWorkflow("w1", "api"), newAgentAt("two", "w1", 2))

		harness.watch(buildPuts(newMark("a", "one")))
		require.Equal(t, []string{"api", "  two"}, readListRows(harness.readFrame()), "the mark draws no row of its own")

		harness.watch(buildPuts(newAgentAt("one", "w1", 1)))
		harness.press("'", "a")

		assert.Equal(t, "  one", readHoveredRow(harness.readFrame()))
	})

	t.Run("a mark's tombstone takes the letter off", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t,
			newLiveWorkflow("w1", "api"),
			newAgentAt("two", "w1", 2),
			newAgentAt("one", "w1", 1),
			newMark("a", "one"),
		)
		harness.press("'", "a")
		require.Equal(t, "  one", readHoveredRow(harness.readFrame()))
		harness.press("k")

		harness.watch(buildDeleted(watchv1.Resource_RESOURCE_MARK, "m-one"))
		harness.press("'", "a")

		assert.Equal(t, "  two", readHoveredRow(harness.readFrame()))
	})

	t.Run("an agent's tombstone takes its children and its mark with it", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t,
			newLiveWorkflow("w1", "api"),
			newAgent("a1", "parent", "w1"),
			newChild("a2", "child", "a1"),
			newAgent("a3", "other", "w1"),
			newMark("a", "a1"),
		)
		require.Len(t, readListRows(harness.readFrame()), 4)

		harness.watch(
			buildDeleted(watchv1.Resource_RESOURCE_AGENT, "a1"),
			buildDeleted(watchv1.Resource_RESOURCE_MARK, "m-a1"),
			buildDeleted(watchv1.Resource_RESOURCE_AGENT, "a2"),
		)

		assert.Equal(t, []string{"api", "  other"}, readListRows(harness.readFrame()))
	})

	t.Run("a put lands over the row it names and the cursor stays on it", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t,
			newLiveWorkflow("w1", "api"),
			newAgentAt("one", "w1", 1),
			newAgentAt("two", "w1", 2),
		)
		harness.press("j")
		require.Equal(t, "  one", readHoveredRow(harness.readFrame()))

		renamed := newAgentAt("one", "w1", 1)
		renamed.Name = "renamed"
		harness.watch(
			buildPuts(renamed, newAgentAt("three", "w1", 3), newLiveWorkflow("w0", "aaa")),
			buildDeleted(watchv1.Resource_RESOURCE_AGENT, "two"),
		)

		frame := harness.readFrame()
		assert.Equal(t, []string{"aaa", "api", "  three", "  renamed"}, readListRows(frame))
		assert.Equal(t, "  renamed", readHoveredRow(frame))
	})

	t.Run("a workflow renamed out of its place is filed again", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, newLiveWorkflow("w1", "api"), newLiveWorkflow("w2", "web"))

		harness.watch(buildPuts(newLiveWorkflow("w1", "zzz")))

		assert.Equal(t, []string{"web", "zzz"}, readListRows(harness.readFrame()))
	})

	t.Run("a workflow's tombstone takes it off the list", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, newLiveWorkflow("w0", "aaa"), newLiveWorkflow("w1", "api"))

		harness.watch(buildDeleted(watchv1.Resource_RESOURCE_WORKFLOW, "w0"))

		assert.Equal(t, []string{"api"}, readListRows(harness.readFrame()))
	})

	t.Run("a schedule's tombstone takes it off the schedules screen", func(t *testing.T) {
		t.Parallel()

		harness := openSchedules(t, buildSchedules()...)
		require.Len(t, readScheduleRows(harness.readFrame()), 1)

		harness.watch(buildDeleted(watchv1.Resource_RESOURCE_SCHEDULE, "s1"))

		assert.Empty(t, readScheduleRows(harness.readFrame()))
	})

	t.Run("unclaimed mail is marked until it expires by the panel's clock or is claimed", func(t *testing.T) {
		t.Parallel()

		expired := newUnreadMail("expired")
		expired.Id = "m2"
		expired.ExpiresAt = xptr.Ptr(time.Now().Add(-time.Hour).UTC().Format(time.RFC3339))
		harness := openAgents(t,
			newLiveWorkflow("w1", "api"),
			newAgentAt("waiting", "w1", 2),
			newAgentAt("expired", "w1", 1),
			newUnreadMail("waiting"),
			expired,
		)

		frame := harness.readFrame()
		assert.Contains(t, findRow(readListRows(frame), "waiting"), mailGlyph)
		assert.NotContains(t, findRow(readListRows(frame), "expired"), mailGlyph)

		harness.watch(buildDeleted(watchv1.Resource_RESOURCE_MAIL, "m1"))

		assert.NotContains(t, findRow(readListRows(harness.readFrame()), "waiting"), mailGlyph)
	})

	t.Run("a worktree's tombstone takes it off the worktrees screen", func(t *testing.T) {
		t.Parallel()

		harness := openWorktrees(t)
		require.Contains(t, harness.readFrame(), "agent/orphan")

		harness.watch(buildDeleted(watchv1.Resource_RESOURCE_WORKTREE, findOrphanDir(t)))

		assert.NotContains(t, harness.readFrame(), "agent/orphan")
	})
}

func buildDeleted(resource watchv1.Resource, key string) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Changes{Changes: &watchv1.Changes{Deleted: []*watchv1.Deleted{{
		Resource: resource,
		Key:      key,
	}}}}}
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
