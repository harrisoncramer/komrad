package pullrequests

import (
	"slices"
	"testing"
	"time"

	"komrad/internal/forge"
	mailstore "komrad/internal/store/mail"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAPullRequestChangeTellsTheAgentsOnItsBranch pins what the one unread notice on a pull request says after each pass.
func TestAPullRequestChangeTellsTheAgentsOnItsBranch(t *testing.T) {
	pending := forge.Standing{Decision: forge.ReviewDecisionPending, Checks: forge.ChecksPending}

	tests := []struct {
		name      string
		standings []forge.Standing
		wantLines []string
	}{
		{
			name:      "checks failing",
			standings: []forge.Standing{{Decision: forge.ReviewDecisionPending, Checks: forge.ChecksFailing}},
			wantLines: []string{"  checks: failing\n"},
		},
		{
			name:      "an approval",
			standings: []forge.Standing{{Decision: forge.ReviewDecisionApproved, Checks: forge.ChecksPending}},
			wantLines: []string{"  review: approved\n"},
		},
		{
			name:      "checks passing is not news on its own",
			standings: []forge.Standing{{Decision: forge.ReviewDecisionPending, Checks: forge.ChecksPassing}},
		},
		{
			name: "two changes before the agent reads are one notice",
			standings: []forge.Standing{
				{Decision: forge.ReviewDecisionPending, Checks: forge.ChecksFailing},
				{Decision: forge.ReviewDecisionPending, Checks: forge.ChecksFailing, Mergeability: forge.MergeabilityConflicting},
			},
			wantLines: []string{"  checks: failing\n", "  mergeability: conflicting with main\n"},
		},
		{
			name: "a later change rewrites the notice to where things stand",
			standings: []forge.Standing{
				{Decision: forge.ReviewDecisionPending, Checks: forge.ChecksFailing},
				{Decision: forge.ReviewDecisionPending, Checks: forge.ChecksPassing},
			},
			wantLines: []string{"  checks: passing\n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSyncFixture(t)
			agentID := f.agentOn(t, "a", "agent/x")
			f.forge.updated[""] = &forge.Page{PullRequests: []forge.PullRequest{pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Minute))}}
			f.forge.standings[1] = pending
			f.sync(t)

			for i, standing := range tt.standings {
				f.setStanding(t, standing, syncNow.Add(time.Duration(i+1)*time.Minute))
			}

			told := f.listMail(t, agentID)
			if tt.wantLines == nil {
				assert.Empty(t, told)

				return
			}
			require.Len(t, told, 1)
			assert.Equal(t, mailstore.KindPullRequestChanged, told[0].Kind)
			for _, line := range tt.wantLines {
				assert.Contains(t, told[0].Body, line)
			}
		})
	}
}

// TestAPullRequestChangeAfterTheNoticeWasReadStartsAnother pins that a read notice is left alone.
func TestAPullRequestChangeAfterTheNoticeWasReadStartsAnother(t *testing.T) {
	f := newSyncFixture(t)
	agentID := f.agentOn(t, "a", "agent/x")
	f.forge.updated[""] = &forge.Page{PullRequests: []forge.PullRequest{pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Minute))}}
	f.forge.standings[1] = forge.Standing{Decision: forge.ReviewDecisionPending, Checks: forge.ChecksPending}
	f.sync(t)

	f.setStanding(t, forge.Standing{Decision: forge.ReviewDecisionPending, Checks: forge.ChecksFailing}, syncNow.Add(time.Minute))
	claimed, err := f.stores.Mail.Claim(t.Context(), agentID, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	f.setStanding(t, forge.Standing{Decision: forge.ReviewDecisionApproved, Checks: forge.ChecksFailing}, syncNow.Add(2*time.Minute))

	told := f.listMail(t, agentID)
	require.Len(t, told, 2)
	assert.Contains(t, told[1].Body, "  review: approved\n")
	assert.Contains(t, told[1].Body, "  checks: failing\n")
}

func (f *syncFixture) listMail(t *testing.T, agentID string) []mailstore.Mail {
	t.Helper()

	listed, err := f.stores.Mail.ListPage(t.Context(), agentID, 200, nil)
	require.NoError(t, err)

	slices.Reverse(listed)

	return listed
}

// setStanding records what the forge now says of PR_1.
func (f *syncFixture) setStanding(t *testing.T, standing forge.Standing, readAt time.Time) {
	t.Helper()

	if standing.Mergeability == forge.MergeabilityUnknown {
		standing.Mergeability = forge.MergeabilityClean
	}
	require.NoError(t, f.syncer.setStanding(t.Context(), f.stored(t, "PR_1").ID, standing, readAt))
}
