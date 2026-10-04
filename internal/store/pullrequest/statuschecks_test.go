package pullrequest

import (
	"testing"
	"time"

	"komrad/internal/forge"
	"komrad/internal/store"
	"komrad/lib/xptr"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var statusCheckReadAt = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

func statusCheckRun(providerID, name string, state forge.StatusCheckState, conclusion string) forge.StatusCheck {
	return forge.StatusCheck{
		ProviderID:   providerID,
		Kind:         forge.StatusCheckKindRun,
		Source:       "CI",
		Name:         name,
		State:        state,
		Conclusion:   conclusion,
		StartedAt:    xptr.Ptr(statusCheckReadAt.Add(-time.Minute)),
		Required:     true,
		URL:          "https://github.com/harrisoncramer/komrad/runs/" + providerID,
		RunID:        "CS_1",
		RunEvent:     "pull_request",
		RunStartedAt: xptr.Ptr(statusCheckReadAt.Add(-2 * time.Minute)),
	}
}

func inRun(check forge.StatusCheck, runID string) forge.StatusCheck {
	check.RunID = runID

	return check
}

func statusCheckNames(reading StatusCheckReading) []string {
	return xslice.Map(reading.StatusChecks, func(check forge.StatusCheck) string { return check.Name + ":" + string(check.State) })
}

// TestSetStatusChecks covers what each read of a head commit leaves listed.
func TestSetStatusChecks(t *testing.T) {
	tests := []struct {
		name  string
		reads []SetStatusChecksParams
		want  []StatusCheckReading
	}{
		{
			name: "a read lists the checks on its head",
			reads: []SetStatusChecksParams{
				{HeadSHA: "c1", StatusChecks: []forge.StatusCheck{statusCheckRun("1", "test", forge.StatusCheckStateRunning, ""), statusCheckRun("2", "lint", forge.StatusCheckStateQueued, "")}},
			},
			want: []StatusCheckReading{{HeadSHA: "c1", StatusChecks: []forge.StatusCheck{statusCheckRun("2", "lint", forge.StatusCheckStateQueued, ""), statusCheckRun("1", "test", forge.StatusCheckStateRunning, "")}}},
		},
		{
			name: "a new head keeps the reading of the old one",
			reads: []SetStatusChecksParams{
				{HeadSHA: "c1", StatusChecks: []forge.StatusCheck{statusCheckRun("1", "test", forge.StatusCheckStateCompleted, "failure")}},
				{HeadSHA: "c2", StatusChecks: []forge.StatusCheck{statusCheckRun("3", "test", forge.StatusCheckStateRunning, "")}},
			},
			want: []StatusCheckReading{
				{HeadSHA: "c1", StatusChecks: []forge.StatusCheck{statusCheckRun("1", "test", forge.StatusCheckStateCompleted, "failure")}},
				{HeadSHA: "c2", StatusChecks: []forge.StatusCheck{statusCheckRun("3", "test", forge.StatusCheckStateRunning, "")}},
			},
		},
		{
			name: "a later read on the same head moves its checks along",
			reads: []SetStatusChecksParams{
				{HeadSHA: "c1", StatusChecks: []forge.StatusCheck{statusCheckRun("1", "test", forge.StatusCheckStateRunning, "")}},
				{HeadSHA: "c1", StatusChecks: []forge.StatusCheck{statusCheckRun("1", "test", forge.StatusCheckStateCompleted, "success")}},
			},
			want: []StatusCheckReading{{HeadSHA: "c1", StatusChecks: []forge.StatusCheck{statusCheckRun("1", "test", forge.StatusCheckStateCompleted, "success")}}},
		},
		{
			name: "a later read that moves a check to another run rewrites it",
			reads: []SetStatusChecksParams{
				{HeadSHA: "c1", StatusChecks: []forge.StatusCheck{statusCheckRun("1", "test", forge.StatusCheckStateCompleted, "success")}},
				{HeadSHA: "c1", StatusChecks: []forge.StatusCheck{inRun(statusCheckRun("1", "test", forge.StatusCheckStateCompleted, "success"), "CS_2")}},
			},
			want: []StatusCheckReading{{HeadSHA: "c1", StatusChecks: []forge.StatusCheck{inRun(statusCheckRun("1", "test", forge.StatusCheckStateCompleted, "success"), "CS_2")}}},
		},
		{
			name: "a check the forge stops listing goes",
			reads: []SetStatusChecksParams{
				{HeadSHA: "c1", StatusChecks: []forge.StatusCheck{statusCheckRun("1", "test", forge.StatusCheckStateCompleted, "failure"), statusCheckRun("2", "lint", forge.StatusCheckStateCompleted, "success")}},
				{HeadSHA: "c1", StatusChecks: []forge.StatusCheck{statusCheckRun("4", "test", forge.StatusCheckStateRunning, ""), statusCheckRun("2", "lint", forge.StatusCheckStateCompleted, "success")}},
			},
			want: []StatusCheckReading{{HeadSHA: "c1", StatusChecks: []forge.StatusCheck{statusCheckRun("2", "lint", forge.StatusCheckStateCompleted, "success"), statusCheckRun("4", "test", forge.StatusCheckStateRunning, "")}}},
		},
		{
			name: "a truncated read keeps the checks it left out",
			reads: []SetStatusChecksParams{
				{HeadSHA: "c1", StatusChecks: []forge.StatusCheck{statusCheckRun("1", "test", forge.StatusCheckStateRunning, ""), statusCheckRun("2", "lint", forge.StatusCheckStateQueued, "")}},
				{
					HeadSHA:      "c1",
					StatusChecks: []forge.StatusCheck{statusCheckRun("1", "test", forge.StatusCheckStateCompleted, "success")},
					Truncated:    true,
				},
			},
			want: []StatusCheckReading{{HeadSHA: "c1", StatusChecks: []forge.StatusCheck{statusCheckRun("2", "lint", forge.StatusCheckStateQueued, ""), statusCheckRun("1", "test", forge.StatusCheckStateCompleted, "success")}}},
		},
		{
			name: "a check listed twice in one read is kept once as last listed",
			reads: []SetStatusChecksParams{
				{HeadSHA: "c1", StatusChecks: []forge.StatusCheck{statusCheckRun("1", "test", forge.StatusCheckStateRunning, ""), statusCheckRun("1", "test", forge.StatusCheckStateCompleted, "failure")}},
			},
			want: []StatusCheckReading{{HeadSHA: "c1", StatusChecks: []forge.StatusCheck{statusCheckRun("1", "test", forge.StatusCheckStateCompleted, "failure")}}},
		},
		{
			name: "a blank read keeps the checks listed before it",
			reads: []SetStatusChecksParams{
				{HeadSHA: "c1", StatusChecks: []forge.StatusCheck{statusCheckRun("1", "test", forge.StatusCheckStateRunning, "")}},
				{HeadSHA: "c1"},
			},
			want: []StatusCheckReading{{
				HeadSHA:      "c1",
				Blank:        true,
				StatusChecks: []forge.StatusCheck{statusCheckRun("1", "test", forge.StatusCheckStateRunning, "")},
			}},
		},
		{
			name:  "a first read that is blank is kept as blank",
			reads: []SetStatusChecksParams{{HeadSHA: "c1"}},
			want:  []StatusCheckReading{{HeadSHA: "c1", Blank: true}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := open(t)
			checks := NewStatusCheckStore(h.pool.GetDB())
			ctx := t.Context()
			written, err := h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
			require.NoError(t, err)

			for i, read := range tt.reads {
				read.PullRequestID = written.ID
				read.ReadAt = statusCheckReadAt.Add(time.Duration(i) * time.Minute)
				require.NoError(t, checks.SetStatusChecks(ctx, read))
			}

			got, err := checks.ListOfPullRequest(ctx, written.ID)
			require.NoError(t, err)
			require.Len(t, got, len(tt.want))
			for i, want := range tt.want {
				assert.Equal(t, want.HeadSHA, got[i].HeadSHA)
				assert.Equal(t, want.Blank, got[i].Blank)
				assert.Equal(t, statusCheckNames(want), statusCheckNames(got[i]))
				for j, check := range got[i].StatusChecks {
					check.StartedAt = xptr.Ptr(check.StartedAt.UTC())
					check.RunStartedAt = xptr.Ptr(check.RunStartedAt.UTC())
					assert.Equal(t, want.StatusChecks[j], check)
				}
			}
		})
	}
}

func statusCheckStep(number int, name string, state forge.StatusCheckState, conclusion string) forge.StatusCheckStep {
	return forge.StatusCheckStep{
		Number:     number,
		Name:       name,
		State:      state,
		Conclusion: conclusion,
		StartedAt:  xptr.Ptr(statusCheckReadAt.Add(-time.Minute)),
	}
}

func withSteps(check forge.StatusCheck, steps ...forge.StatusCheckStep) forge.StatusCheck {
	check.Steps = steps

	return check
}

func truncatedSteps(check forge.StatusCheck) forge.StatusCheck {
	check.StepsTruncated = true

	return check
}

// TestSetStatusCheckSteps covers what each read of a head commit leaves listed under each check.
func TestSetStatusCheckSteps(t *testing.T) {
	running := statusCheckRun("1", "test", forge.StatusCheckStateRunning, "")
	tests := []struct {
		name  string
		reads [][]forge.StatusCheck
		want  map[string][]forge.StatusCheckStep
	}{
		{
			name: "a read lists the steps of each check in order",
			reads: [][]forge.StatusCheck{{
				withSteps(running, statusCheckStep(2, "Run tests", forge.StatusCheckStateRunning, ""), statusCheckStep(1, "Set up job", forge.StatusCheckStateCompleted, "success")),
				statusCheckRun("2", "lint", forge.StatusCheckStateQueued, ""),
			}},
			want: map[string][]forge.StatusCheckStep{
				"1": {statusCheckStep(1, "Set up job", forge.StatusCheckStateCompleted, "success"), statusCheckStep(2, "Run tests", forge.StatusCheckStateRunning, "")},
			},
		},
		{
			name: "a later read moves a step along",
			reads: [][]forge.StatusCheck{
				{withSteps(running, statusCheckStep(1, "Run tests", forge.StatusCheckStateRunning, ""))},
				{withSteps(running, statusCheckStep(1, "Run tests", forge.StatusCheckStateCompleted, "failure"))},
			},
			want: map[string][]forge.StatusCheckStep{
				"1": {statusCheckStep(1, "Run tests", forge.StatusCheckStateCompleted, "failure")},
			},
		},
		{
			name: "a step the forge stops listing goes",
			reads: [][]forge.StatusCheck{
				{withSteps(running, statusCheckStep(1, "Set up job", forge.StatusCheckStateCompleted, "success"), statusCheckStep(2, "Run tests", forge.StatusCheckStateRunning, ""))},
				{withSteps(running, statusCheckStep(1, "Set up job", forge.StatusCheckStateCompleted, "success"))},
			},
			want: map[string][]forge.StatusCheckStep{
				"1": {statusCheckStep(1, "Set up job", forge.StatusCheckStateCompleted, "success")},
			},
		},
		{
			name: "a check whose steps were truncated keeps the steps left out",
			reads: [][]forge.StatusCheck{
				{withSteps(running, statusCheckStep(1, "Set up job", forge.StatusCheckStateCompleted, "success"), statusCheckStep(2, "Run tests", forge.StatusCheckStateRunning, ""))},
				{truncatedSteps(withSteps(running, statusCheckStep(1, "Set up job", forge.StatusCheckStateCompleted, "success")))},
			},
			want: map[string][]forge.StatusCheckStep{
				"1": {statusCheckStep(1, "Set up job", forge.StatusCheckStateCompleted, "success"), statusCheckStep(2, "Run tests", forge.StatusCheckStateRunning, "")},
			},
		},
		{
			name: "a step listed twice in one read is kept once as last listed",
			reads: [][]forge.StatusCheck{{
				withSteps(running, statusCheckStep(1, "Run tests", forge.StatusCheckStateRunning, ""), statusCheckStep(1, "Run tests", forge.StatusCheckStateCompleted, "success")),
			}},
			want: map[string][]forge.StatusCheckStep{
				"1": {statusCheckStep(1, "Run tests", forge.StatusCheckStateCompleted, "success")},
			},
		},
		{
			name: "a blank read keeps the steps listed before it",
			reads: [][]forge.StatusCheck{
				{withSteps(running, statusCheckStep(1, "Run tests", forge.StatusCheckStateRunning, ""))},
				nil,
			},
			want: map[string][]forge.StatusCheckStep{
				"1": {statusCheckStep(1, "Run tests", forge.StatusCheckStateRunning, "")},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := open(t)
			checks := NewStatusCheckStore(h.pool.GetDB())
			ctx := t.Context()
			written, err := h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
			require.NoError(t, err)

			for i, read := range tt.reads {
				require.NoError(t, checks.SetStatusChecks(ctx, SetStatusChecksParams{
					PullRequestID: written.ID,
					HeadSHA:       "c1",
					StatusChecks:  read,
					ReadAt:        statusCheckReadAt.Add(time.Duration(i) * time.Minute),
				}))
			}

			got, err := checks.ListOfPullRequest(ctx, written.ID)
			require.NoError(t, err)
			require.Len(t, got, 1)
			steps := map[string][]forge.StatusCheckStep{}
			for _, check := range got[0].StatusChecks {
				if len(check.Steps) == 0 {
					continue
				}
				steps[check.ProviderID] = xslice.Map(check.Steps, func(step forge.StatusCheckStep) forge.StatusCheckStep {
					step.StartedAt = xptr.Ptr(step.StartedAt.UTC())

					return step
				})
			}
			assert.Equal(t, tt.want, steps)
		})
	}
}

// TestSetStatusChecksUnchangedStampsOnlyTheReadTime covers a poll that finds nothing new for a head.
func TestSetStatusChecksUnchangedStampsOnlyTheReadTime(t *testing.T) {
	h := open(t)
	checks := NewStatusCheckStore(h.pool.GetDB())
	ctx := t.Context()
	written, err := h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)
	versions := func() []string {
		t.Helper()
		rows, err := h.pool.GetDB().Query(ctx, "SELECT xmin::text FROM pull_request_checks UNION ALL SELECT xmin::text FROM pull_request_check_steps ORDER BY 1")
		require.NoError(t, err)
		defer rows.Close()
		var got []string
		for rows.Next() {
			var xmin string
			require.NoError(t, rows.Scan(&xmin))
			got = append(got, xmin)
		}
		require.NoError(t, rows.Err())

		return got
	}
	params := SetStatusChecksParams{
		PullRequestID: written.ID,
		HeadSHA:       "c1",
		StatusChecks:  []forge.StatusCheck{withSteps(statusCheckRun("1", "test", forge.StatusCheckStateRunning, ""), statusCheckStep(1, "Run tests", forge.StatusCheckStateRunning, "")), statusCheckRun("2", "lint", forge.StatusCheckStateQueued, "")},
		ReadAt:        statusCheckReadAt,
	}

	require.NoError(t, checks.SetStatusChecks(ctx, params))
	before := versions()
	params.ReadAt = statusCheckReadAt.Add(time.Minute)
	require.NoError(t, checks.SetStatusChecks(ctx, params))

	assert.Equal(t, before, versions())
	got, err := checks.ListOfPullRequest(ctx, written.ID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.True(t, got[0].FirstReadAt.Equal(statusCheckReadAt))
	assert.True(t, got[0].ReadAt.Equal(statusCheckReadAt.Add(time.Minute)))
}

// TestStatusChecksGoWithThePullRequest covers the readings and checks going when their pull request does.
func TestStatusChecksGoWithThePullRequest(t *testing.T) {
	h := open(t)
	checks := NewStatusCheckStore(h.pool.GetDB())
	ctx := t.Context()
	written, err := h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)
	require.NoError(t, checks.SetStatusChecks(ctx, SetStatusChecksParams{
		PullRequestID: written.ID,
		HeadSHA:       "c1",
		StatusChecks:  []forge.StatusCheck{withSteps(statusCheckRun("1", "test", forge.StatusCheckStateRunning, ""), statusCheckStep(1, "Run tests", forge.StatusCheckStateRunning, ""))},
		ReadAt:        statusCheckReadAt,
	}))

	_, err = h.pool.GetDB().Exec(ctx, "DELETE FROM pull_requests WHERE id = $1", written.ID)
	require.NoError(t, err)

	var readings, rows, steps int
	require.NoError(t, h.pool.GetDB().QueryRow(ctx, "SELECT (SELECT count(*) FROM pull_request_check_readings), (SELECT count(*) FROM pull_request_checks), (SELECT count(*) FROM pull_request_check_steps)").Scan(&readings, &rows, &steps))
	assert.Zero(t, readings)
	assert.Zero(t, rows)
	assert.Zero(t, steps)
}

// TestMarkUnknown covers which checks and steps a superseded head settles as unknown.
func TestMarkUnknown(t *testing.T) {
	h := open(t)
	checks := NewStatusCheckStore(h.pool.GetDB())
	ctx := t.Context()
	written, err := h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)
	listed := []forge.StatusCheck{
		withSteps(statusCheckRun("1", "test", forge.StatusCheckStateRunning, ""),
			statusCheckStep(1, "Set up job", forge.StatusCheckStateCompleted, "success"),
			statusCheckStep(2, "Run tests", forge.StatusCheckStateRunning, ""),
			statusCheckStep(3, "Upload", forge.StatusCheckStateQueued, "")),
		statusCheckRun("2", "lint", forge.StatusCheckStateCompleted, "success"),
		statusCheckRun("3", "deploy", forge.StatusCheckStateWaiting, ""),
		statusCheckRun("4", "build", forge.StatusCheckStateQueued, ""),
	}
	for _, head := range []string{"old", "other", "abc"} {
		require.NoError(t, checks.SetStatusChecks(ctx, SetStatusChecksParams{
			PullRequestID: written.ID,
			HeadSHA:       head,
			StatusChecks:  listed,
			ReadAt:        statusCheckReadAt,
		}))
	}
	settledAt := statusCheckReadAt.Add(time.Hour)

	for _, head := range []string{"old", "abc"} {
		require.NoError(t, checks.MarkUnknown(ctx, MarkUnknownParams{
			PullRequestID: written.ID,
			HeadSHA:       head,
			ReadAt:        settledAt,
		}))
	}

	got, err := checks.ListOfPullRequest(ctx, written.ID)
	require.NoError(t, err)
	byHead := map[string]StatusCheckReading{}
	for _, reading := range got {
		byHead[reading.HeadSHA] = reading
	}
	stepStates := func(reading StatusCheckReading) []string {
		return xslice.Map(reading.StatusChecks[3].Steps, func(step forge.StatusCheckStep) string { return step.Name + ":" + string(step.State) })
	}
	assert.Equal(t, []string{"build:unknown", "deploy:unknown", "lint:completed", "test:unknown"}, statusCheckNames(byHead["old"]))
	assert.Equal(t, []string{"Set up job:completed", "Run tests:unknown", "Upload:unknown"}, stepStates(byHead["old"]))
	assert.True(t, byHead["old"].ReadAt.Equal(settledAt))
	for _, head := range []string{"other", "abc"} {
		assert.Equal(t, []string{"build:queued", "deploy:waiting", "lint:completed", "test:running"}, statusCheckNames(byHead[head]), head)
		assert.Equal(t, []string{"Set up job:completed", "Run tests:running", "Upload:queued"}, stepStates(byHead[head]), head)
		assert.True(t, byHead[head].ReadAt.Equal(statusCheckReadAt), head)
	}
}

// TestGetOfHead covers which reading a head answers with.
func TestGetOfHead(t *testing.T) {
	h := open(t)
	checks := NewStatusCheckStore(h.pool.GetDB())
	ctx := t.Context()
	written, err := h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)
	for head, state := range map[string]forge.StatusCheckState{"old": forge.StatusCheckStateRunning, "abc": forge.StatusCheckStateCompleted} {
		require.NoError(t, checks.SetStatusChecks(ctx, SetStatusChecksParams{
			PullRequestID: written.ID,
			HeadSHA:       head,
			StatusChecks:  []forge.StatusCheck{statusCheckRun("1", "test", state, "")},
			ReadAt:        statusCheckReadAt,
		}))
	}

	got, err := checks.GetOfHead(ctx, written.ID, "old")
	require.NoError(t, err)
	assert.Equal(t, "old", got.HeadSHA)
	assert.Equal(t, []string{"test:running"}, statusCheckNames(*got))

	_, err = checks.GetOfHead(ctx, written.ID, "never")
	require.ErrorIs(t, err, store.ErrNotFound)

	_, err = checks.GetOfHead(ctx, "not-an-id", "old")
	require.ErrorIs(t, err, store.ErrNotFound)
}
