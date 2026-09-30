package pullrequests

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"komrad/config"
	"komrad/internal/events"
	"komrad/internal/forge"
	"komrad/internal/mail"
	mailstore "komrad/internal/store/mail"
	pullrequeststore "komrad/internal/store/pullrequest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAPullRequestChangeTellsTheAgentsOnItsBranch pins what the one unread mail about a pull request says after each pass.
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
			name: "two changes before the agent reads are one mail",
			standings: []forge.Standing{
				{Decision: forge.ReviewDecisionPending, Checks: forge.ChecksFailing},
				{
					Decision:     forge.ReviewDecisionPending,
					Checks:       forge.ChecksFailing,
					Mergeability: forge.MergeabilityConflicting,
				},
			},
			wantLines: []string{"  checks: failing\n", "  mergeability: conflicting with main\n"},
		},
		{
			name: "a later change rewrites the mail to where things stand",
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

// TestAPullRequestChangeAfterTheMailWasReadStartsAnother pins that mail the agent read is left alone.
func TestAPullRequestChangeAfterTheMailWasReadStartsAnother(t *testing.T) {
	failingOnA := forge.Standing{
		Decision: forge.ReviewDecisionPending,
		Checks:   forge.ChecksFailing,
		HeadSHA:  "aaa",
	}

	tests := []struct {
		name      string
		after     forge.Standing
		wantLines []string
	}{
		{
			name: "an approval",
			after: forge.Standing{
				Decision: forge.ReviewDecisionApproved,
				Checks:   forge.ChecksFailing,
				HeadSHA:  "aaa",
			},
			wantLines: []string{"  review: approved\n", "  checks: failing\n"},
		},
		{
			name: "the checks failing again on a new head",
			after: forge.Standing{
				Decision: forge.ReviewDecisionPending,
				Checks:   forge.ChecksFailing,
				HeadSHA:  "bbb",
			},
			wantLines: []string{"  checks: failing\n"},
		},
		{
			name:  "the checks failing again on the same head is not news",
			after: failingOnA,
		},
		{
			name: "the checks passing on a new head is not news",
			after: forge.Standing{
				Decision: forge.ReviewDecisionPending,
				Checks:   forge.ChecksPassing,
				HeadSHA:  "bbb",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSyncFixture(t)
			agentID := f.agentOn(t, "a", "agent/x")
			f.forge.updated[""] = &forge.Page{PullRequests: []forge.PullRequest{pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Minute))}}
			f.forge.standings[1] = forge.Standing{Decision: forge.ReviewDecisionPending, Checks: forge.ChecksPending}
			f.sync(t)

			f.setStanding(t, failingOnA, syncNow.Add(time.Minute))
			claimed, err := f.stores.Mail.Claim(t.Context(), agentID, 10)
			require.NoError(t, err)
			require.Len(t, claimed, 1)

			f.setStanding(t, tt.after, syncNow.Add(2*time.Minute))

			told := f.listMail(t, agentID)
			if tt.wantLines == nil {
				assert.Len(t, told, 1)

				return
			}
			require.Len(t, told, 2)
			for _, line := range tt.wantLines {
				assert.Contains(t, told[1].Body, line)
			}
		})
	}
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
	require.NoError(t, f.syncer.setStanding(t.Context(), pullrequeststore.SetStandingParams{
		ID:       f.stored(t, "PR_1").ID,
		Standing: standing,
		ReadAt:   readAt,
	}))
}

// TestAPullRequestChangeRetriesAFullMailboxAlone pins one agent's full mailbox leaving the others on the branch told and its own mail retried once there is room.
func TestAPullRequestChangeRetriesAFullMailboxAlone(t *testing.T) {
	f := newSyncFixture(t)
	told := f.agentOn(t, "a", "agent/x")
	full := f.agentOn(t, "b", "agent/x")
	f.forge.updated[""] = &forge.Page{PullRequests: []forge.PullRequest{pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Minute))}}
	f.forge.standings[1] = forge.Standing{Decision: forge.ReviewDecisionPending, Checks: forge.ChecksPending}
	f.sync(t)
	f.fillMailbox(t, full)

	f.setStanding(t, forge.Standing{Decision: forge.ReviewDecisionPending, Checks: forge.ChecksFailing}, syncNow.Add(time.Minute))

	assert.Empty(t, f.events.TakeErrors())
	require.Len(t, f.listMail(t, told), 1)
	followUps := f.events.ListFollowUps()
	require.Len(t, followUps, 1)
	retry, ok := followUps[0].(events.Event)
	require.True(t, ok)
	var change Change
	require.NoError(t, json.Unmarshal(retry.Payload, &change))
	assert.Equal(t, full, change.Recipient)

	_, err := f.stores.Mail.Claim(t.Context(), full, 200)
	require.NoError(t, err)
	_, err = f.stores.Mail.Claim(t.Context(), told, 10)
	require.NoError(t, err)
	require.NoError(t, f.events.InsertTx(t.Context(), retry))

	assert.Empty(t, f.events.TakeErrors())
	require.Len(t, f.listMail(t, told), 1, "the retry tells only the agent it was for")
	var pullRequestMail []mailstore.Mail
	for _, held := range f.listMail(t, full) {
		if held.Kind == mailstore.KindPullRequestChanged {
			pullRequestMail = append(pullRequestMail, held)
		}
	}
	require.Len(t, pullRequestMail, 1)
	assert.Contains(t, pullRequestMail[0].Body, "  checks: failing\n")
}

func (f *syncFixture) fillMailbox(t *testing.T, agentID string) {
	t.Helper()

	mailer := mail.NewMailService(mail.NewMailServiceParams{
		MailReadWriter: f.stores.Mail,
		Agents:         f.stores.Agents,
		Config:         config.Mail{},
	})
	for {
		_, err := mailer.SendFromKomrad(t.Context(), mail.SendFromKomradParams{
			ToAgentID: agentID,
			Body:      "waiting",
			Kind:      mailstore.KindReviewActivity,
			SubjectID: uuid.NewString(),
		})
		if errors.Is(err, mail.ErrMailboxFull) {
			return
		}
		require.NoError(t, err)
	}
}
