package pullrequest

import (
	"testing"
	"time"

	"komrad/internal/forge"
	"komrad/lib/xslice"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type checkFailureHarness struct {
	*harness
	checks        *StatusCheckStore
	pullRequestID uuid.UUID
	agentID       uuid.UUID
	otherAgentID  uuid.UUID
}

func openCheckFailures(t *testing.T) *checkFailureHarness {
	t.Helper()

	h := open(t)
	workflow := h.createWorkflow(t)
	written, err := h.Upsert(t.Context(), openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)

	return &checkFailureHarness{
		harness:       h,
		checks:        NewStatusCheckStore(h.pool.GetDB()),
		pullRequestID: written.ID,
		agentID:       h.createAgent(t, workflow, "author", komrad, "agent/x"),
		otherAgentID:  h.createAgent(t, workflow, "other", komrad, "agent/x"),
	}
}

func (h *checkFailureHarness) read(t *testing.T, headSHA string, checks ...forge.StatusCheck) {
	t.Helper()

	require.NoError(t, h.checks.SetStatusChecks(t.Context(), SetStatusChecksParams{
		PullRequestID: h.pullRequestID,
		HeadSHA:       headSHA,
		StatusChecks:  checks,
		ReadAt:        statusCheckReadAt,
	}))
}

func (h *checkFailureHarness) failures(agentID uuid.UUID, headSHA string, failed ...forge.StatusCheck) ListCheckFailuresParams {
	return ListCheckFailuresParams{
		PullRequestID: h.pullRequestID,
		HeadSHA:       headSHA,
		AgentID:       agentID,
		Failed:        failed,
	}
}

func (h *checkFailureHarness) told(agentID uuid.UUID, told ...forge.StatusCheck) RecordCheckFailuresParams {
	return RecordCheckFailuresParams{
		PullRequestID: h.pullRequestID,
		HeadSHA:       "c1",
		AgentID:       agentID,
		Told:          told,
	}
}

func (h *checkFailureHarness) notedAt(t *testing.T, params ListCheckFailuresParams) []*time.Time {
	t.Helper()

	listed, err := h.checks.ListCheckFailures(t.Context(), params)
	require.NoError(t, err)

	return xslice.Map(listed, func(failure CheckFailure) *time.Time { return failure.NotedAt })
}

func (h *checkFailureHarness) countNotes(t *testing.T) int {
	t.Helper()

	var notes int
	require.NoError(t, h.pool.GetDB().QueryRow(t.Context(), "SELECT count(*) FROM pull_request_check_failure_notes").Scan(&notes))

	return notes
}

func finishedAt(check forge.StatusCheck, at time.Time) forge.StatusCheck {
	check.FinishedAt = &at

	return check
}

// TestCheckFailuresAreToldToEachAgentOnce covers that a note tells one agent of one failure on one head,
// and that noting it again keeps when the agent was first told.
func TestCheckFailuresAreToldToEachAgentOnce(t *testing.T) {
	h := openCheckFailures(t)
	ctx := t.Context()
	failed := finishedAt(statusCheckRun("1", "lint", forge.StatusCheckStateCompleted, "failure"), statusCheckReadAt)
	h.read(t, "c1", failed)

	assert.Equal(t, []*time.Time{nil}, h.notedAt(t, h.failures(h.agentID, "c1", failed)), "a failure no one was told of is not noted")

	require.NoError(t, h.checks.RecordCheckFailures(ctx, h.told(h.agentID, failed)))
	first := h.notedAt(t, h.failures(h.agentID, "c1", failed))
	require.NotNil(t, first[0])

	require.NoError(t, h.checks.RecordCheckFailures(ctx, h.told(h.agentID, failed)))
	assert.Equal(t, first, h.notedAt(t, h.failures(h.agentID, "c1", failed)), "noting again keeps when the agent was first told")
	assert.Equal(t, 1, h.countNotes(t))

	assert.Equal(t, []*time.Time{nil}, h.notedAt(t, h.failures(h.otherAgentID, "c1", failed)), "another agent was not told")

	h.read(t, "c2", failed)
	assert.Equal(t, []*time.Time{nil}, h.notedAt(t, h.failures(h.agentID, "c2", failed)), "the same check on another head was not told")
}

// TestCheckFailureThatFailsAgainIsNew covers that a rerun finishing on the same check row is a failure
// the agent has not heard of.
func TestCheckFailureThatFailsAgainIsNew(t *testing.T) {
	h := openCheckFailures(t)
	ctx := t.Context()
	failed := finishedAt(statusCheckRun("1", "lint", forge.StatusCheckStateCompleted, "failure"), statusCheckReadAt)
	h.read(t, "c1", failed)
	require.NoError(t, h.checks.RecordCheckFailures(ctx, h.told(h.agentID, failed)))

	again := finishedAt(failed, statusCheckReadAt.Add(time.Hour))
	h.read(t, "c1", again)

	assert.Equal(t, []*time.Time{nil}, h.notedAt(t, h.failures(h.agentID, "c1", again)))
}

// TestCheckFailureWithNoFinishTimeIsNotedOnce covers that a failure the forge gave no finish time is
// noted once however often it is noted.
func TestCheckFailureWithNoFinishTimeIsNotedOnce(t *testing.T) {
	h := openCheckFailures(t)
	ctx := t.Context()
	failed := statusCheckRun("1", "lint", forge.StatusCheckStateCompleted, "failure")
	h.read(t, "c1", failed)

	require.NoError(t, h.checks.RecordCheckFailures(ctx, h.told(h.agentID, failed)))
	require.NoError(t, h.checks.RecordCheckFailures(ctx, h.told(h.agentID, failed)))

	assert.Equal(t, 1, h.countNotes(t))
	assert.NotNil(t, h.notedAt(t, h.failures(h.agentID, "c1", failed))[0])
}

// TestCheckFailureNotesGoWithTheirAgentAndPullRequest covers that notes go when the agent told or the
// pull request goes.
func TestCheckFailureNotesGoWithTheirAgentAndPullRequest(t *testing.T) {
	h := openCheckFailures(t)
	ctx := t.Context()
	failed := finishedAt(statusCheckRun("1", "lint", forge.StatusCheckStateCompleted, "failure"), statusCheckReadAt)
	h.read(t, "c1", failed)
	require.NoError(t, h.checks.RecordCheckFailures(ctx, h.told(h.agentID, failed)))
	require.NoError(t, h.checks.RecordCheckFailures(ctx, h.told(h.otherAgentID, failed)))

	_, err := h.pool.GetDB().Exec(ctx, "DELETE FROM agents WHERE id = $1", h.agentID)
	require.NoError(t, err)
	assert.Equal(t, 1, h.countNotes(t))

	_, err = h.pool.GetDB().Exec(ctx, "DELETE FROM pull_requests WHERE id = $1", h.pullRequestID)
	require.NoError(t, err)
	assert.Zero(t, h.countNotes(t))
}
