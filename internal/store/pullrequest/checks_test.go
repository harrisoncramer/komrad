package pullrequest

import (
	"testing"
	"time"

	"komrad/internal/forge"
	"komrad/lib/xptr"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var checkReadAt = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

func checkRun(providerID, name string, state forge.CheckState, conclusion string) forge.Check {
	return forge.Check{
		ProviderID: providerID,
		Kind:       forge.CheckKindRun,
		Source:     "CI",
		Name:       name,
		State:      state,
		Conclusion: conclusion,
		StartedAt:  xptr.Ptr(checkReadAt.Add(-time.Minute)),
		Required:   true,
		URL:        "https://github.com/harrisoncramer/komrad/runs/" + providerID,
	}
}

func checkNames(reading checkReading) []string {
	return xslice.Map(reading.Checks, func(check forge.Check) string { return check.Name + ":" + string(check.State) })
}

// TestSetChecks covers what each read of a head commit leaves listed.
func TestSetChecks(t *testing.T) {
	tests := []struct {
		name  string
		reads []SetChecksParams
		want  []checkReading
	}{
		{
			name: "a read lists the checks on its head",
			reads: []SetChecksParams{
				{HeadSHA: "c1", Checks: []forge.Check{checkRun("1", "test", forge.CheckStateRunning, ""), checkRun("2", "lint", forge.CheckStateQueued, "")}},
			},
			want: []checkReading{{HeadSHA: "c1", Checks: []forge.Check{checkRun("2", "lint", forge.CheckStateQueued, ""), checkRun("1", "test", forge.CheckStateRunning, "")}}},
		},
		{
			name: "a new head keeps the reading of the old one",
			reads: []SetChecksParams{
				{HeadSHA: "c1", Checks: []forge.Check{checkRun("1", "test", forge.CheckStateCompleted, "failure")}},
				{HeadSHA: "c2", Checks: []forge.Check{checkRun("3", "test", forge.CheckStateRunning, "")}},
			},
			want: []checkReading{
				{HeadSHA: "c1", Checks: []forge.Check{checkRun("1", "test", forge.CheckStateCompleted, "failure")}},
				{HeadSHA: "c2", Checks: []forge.Check{checkRun("3", "test", forge.CheckStateRunning, "")}},
			},
		},
		{
			name: "a later read on the same head moves its checks along",
			reads: []SetChecksParams{
				{HeadSHA: "c1", Checks: []forge.Check{checkRun("1", "test", forge.CheckStateRunning, "")}},
				{HeadSHA: "c1", Checks: []forge.Check{checkRun("1", "test", forge.CheckStateCompleted, "success")}},
			},
			want: []checkReading{{HeadSHA: "c1", Checks: []forge.Check{checkRun("1", "test", forge.CheckStateCompleted, "success")}}},
		},
		{
			name: "a check the forge stops listing goes",
			reads: []SetChecksParams{
				{HeadSHA: "c1", Checks: []forge.Check{checkRun("1", "test", forge.CheckStateCompleted, "failure"), checkRun("2", "lint", forge.CheckStateCompleted, "success")}},
				{HeadSHA: "c1", Checks: []forge.Check{checkRun("4", "test", forge.CheckStateRunning, ""), checkRun("2", "lint", forge.CheckStateCompleted, "success")}},
			},
			want: []checkReading{{HeadSHA: "c1", Checks: []forge.Check{checkRun("2", "lint", forge.CheckStateCompleted, "success"), checkRun("4", "test", forge.CheckStateRunning, "")}}},
		},
		{
			name: "a truncated read keeps the checks it left out",
			reads: []SetChecksParams{
				{HeadSHA: "c1", Checks: []forge.Check{checkRun("1", "test", forge.CheckStateRunning, ""), checkRun("2", "lint", forge.CheckStateQueued, "")}},
				{
					HeadSHA:   "c1",
					Checks:    []forge.Check{checkRun("1", "test", forge.CheckStateCompleted, "success")},
					Truncated: true,
				},
			},
			want: []checkReading{{HeadSHA: "c1", Checks: []forge.Check{checkRun("2", "lint", forge.CheckStateQueued, ""), checkRun("1", "test", forge.CheckStateCompleted, "success")}}},
		},
		{
			name: "a check listed twice in one read is kept once as last listed",
			reads: []SetChecksParams{
				{HeadSHA: "c1", Checks: []forge.Check{checkRun("1", "test", forge.CheckStateRunning, ""), checkRun("1", "test", forge.CheckStateCompleted, "failure")}},
			},
			want: []checkReading{{HeadSHA: "c1", Checks: []forge.Check{checkRun("1", "test", forge.CheckStateCompleted, "failure")}}},
		},
		{
			name: "a blank read keeps the checks listed before it",
			reads: []SetChecksParams{
				{HeadSHA: "c1", Checks: []forge.Check{checkRun("1", "test", forge.CheckStateRunning, "")}},
				{HeadSHA: "c1"},
			},
			want: []checkReading{{
				HeadSHA: "c1",
				Blank:   true,
				Checks:  []forge.Check{checkRun("1", "test", forge.CheckStateRunning, "")},
			}},
		},
		{
			name:  "a first read that is blank is kept as blank",
			reads: []SetChecksParams{{HeadSHA: "c1"}},
			want:  []checkReading{{HeadSHA: "c1", Blank: true}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := open(t)
			checks := NewCheckStore(h.pool.GetDB())
			ctx := t.Context()
			written, err := h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
			require.NoError(t, err)

			for i, read := range tt.reads {
				read.PullRequestID = written.ID
				read.ReadAt = checkReadAt.Add(time.Duration(i) * time.Minute)
				require.NoError(t, checks.SetChecks(ctx, read))
			}

			got, err := checks.ListOfPullRequest(ctx, written.ID)
			require.NoError(t, err)
			require.Len(t, got, len(tt.want))
			for i, want := range tt.want {
				assert.Equal(t, want.HeadSHA, got[i].HeadSHA)
				assert.Equal(t, want.Blank, got[i].Blank)
				assert.Equal(t, checkNames(want), checkNames(got[i]))
				for j, check := range got[i].Checks {
					check.StartedAt = xptr.Ptr(check.StartedAt.UTC())
					assert.Equal(t, want.Checks[j], check)
				}
			}
		})
	}
}

// TestSetChecksUnchangedStampsOnlyTheReadTime covers a poll that finds nothing new for a head.
func TestSetChecksUnchangedStampsOnlyTheReadTime(t *testing.T) {
	h := open(t)
	checks := NewCheckStore(h.pool.GetDB())
	ctx := t.Context()
	written, err := h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)
	versions := func() []string {
		t.Helper()
		rows, err := h.pool.GetDB().Query(ctx, "SELECT xmin::text FROM pull_request_checks ORDER BY provider_id")
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
	params := SetChecksParams{
		PullRequestID: written.ID,
		HeadSHA:       "c1",
		Checks:        []forge.Check{checkRun("1", "test", forge.CheckStateRunning, ""), checkRun("2", "lint", forge.CheckStateQueued, "")},
		ReadAt:        checkReadAt,
	}

	require.NoError(t, checks.SetChecks(ctx, params))
	before := versions()
	params.ReadAt = checkReadAt.Add(time.Minute)
	require.NoError(t, checks.SetChecks(ctx, params))

	assert.Equal(t, before, versions())
	got, err := checks.ListOfPullRequest(ctx, written.ID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.True(t, got[0].FirstReadAt.Equal(checkReadAt))
	assert.True(t, got[0].ReadAt.Equal(checkReadAt.Add(time.Minute)))
}

// TestChecksGoWithThePullRequest covers the readings and checks going when their pull request does.
func TestChecksGoWithThePullRequest(t *testing.T) {
	h := open(t)
	checks := NewCheckStore(h.pool.GetDB())
	ctx := t.Context()
	written, err := h.Upsert(ctx, openPullRequest("PR_1", 1, "agent/x"))
	require.NoError(t, err)
	require.NoError(t, checks.SetChecks(ctx, SetChecksParams{
		PullRequestID: written.ID,
		HeadSHA:       "c1",
		Checks:        []forge.Check{checkRun("1", "test", forge.CheckStateRunning, "")},
		ReadAt:        checkReadAt,
	}))

	_, err = h.pool.GetDB().Exec(ctx, "DELETE FROM pull_requests WHERE id = $1", written.ID)
	require.NoError(t, err)

	var readings, rows int
	require.NoError(t, h.pool.GetDB().QueryRow(ctx, "SELECT (SELECT count(*) FROM pull_request_check_readings), (SELECT count(*) FROM pull_request_checks)").Scan(&readings, &rows))
	assert.Zero(t, readings)
	assert.Zero(t, rows)
}
