package pullrequests

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"komrad/config"
	"komrad/internal/eventprompts"
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
	pending := forge.MergeReadiness{
		Decision:     forge.ReviewDecisionPending,
		Checks:       forge.ChecksPending,
		Mergeability: forge.MergeabilityClean,
	}

	tests := []struct {
		name       string
		reads      []forge.MergeReadiness
		wantLines  []string
		wantEvents []string
	}{
		{
			name:       "checks failing",
			reads:      []forge.MergeReadiness{{Decision: forge.ReviewDecisionPending, Checks: forge.ChecksFailing}},
			wantLines:  []string{"  checks: failing on abc\n"},
			wantEvents: []string{eventprompts.PullRequestChecksFailing},
		},
		{
			name:       "checks failing while others still run",
			reads:      []forge.MergeReadiness{{Decision: forge.ReviewDecisionPending, Checks: forge.ChecksFailingRunning}},
			wantLines:  []string{"  checks: failing on abc, others still running\n"},
			wantEvents: []string{eventprompts.PullRequestChecksFailingRunning},
		},
		{
			name: "the run finishing before the agent reads folds the early word into the settled one",
			reads: []forge.MergeReadiness{
				{Decision: forge.ReviewDecisionPending, Checks: forge.ChecksFailingRunning},
				{Decision: forge.ReviewDecisionPending, Checks: forge.ChecksFailing},
			},
			wantLines:  []string{"  checks: failing on abc\n"},
			wantEvents: []string{eventprompts.PullRequestChecksFailing},
		},
		{
			name:      "an approval",
			reads:     []forge.MergeReadiness{{Decision: forge.ReviewDecisionApproved, Checks: forge.ChecksPending}},
			wantLines: []string{"  review: approved\n", "do not fetch them from the forge"},
		},
		{
			name:  "checks passing is not news on its own",
			reads: []forge.MergeReadiness{{Decision: forge.ReviewDecisionPending, Checks: forge.ChecksPassing}},
		},
		{
			name: "two changes before the agent reads are one mail",
			reads: []forge.MergeReadiness{
				{Decision: forge.ReviewDecisionPending, Checks: forge.ChecksFailing},
				{
					Decision:     forge.ReviewDecisionPending,
					Checks:       forge.ChecksFailing,
					Mergeability: forge.MergeabilityConflicting,
				},
			},
			wantLines: []string{"  checks: failing on abc\n", "  mergeability: conflicting with main\n"},
		},
		{
			name: "a later change rewrites the mail to where things stand",
			reads: []forge.MergeReadiness{
				{Decision: forge.ReviewDecisionPending, Checks: forge.ChecksFailing},
				{Decision: forge.ReviewDecisionPending, Checks: forge.ChecksPassing},
			},
			wantLines: []string{"  checks: passing on abc\n"},
		},
		{
			name: "auto-merge turned on",
			reads: []forge.MergeReadiness{{
				Decision:  forge.ReviewDecisionPending,
				Checks:    forge.ChecksPending,
				AutoMerge: forge.AutoMerge{On: true},
			}},
			wantLines: []string{"  auto-merge: on, so whatever is on the branch merges once its checks and reviews pass\n"},
		},
		{
			name: "auto-merge turned off again rewrites the mail",
			reads: []forge.MergeReadiness{
				{
					Decision:  forge.ReviewDecisionPending,
					Checks:    forge.ChecksPending,
					AutoMerge: forge.AutoMerge{On: true},
				},
				pending,
			},
			wantLines: []string{"  auto-merge: off\n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSyncFixture(t)
			agentID := f.agentOn(t, "a", "agent/x")
			f.hold(withMergeReadiness(pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Minute)), pending))
			f.sync(t)

			for i, readiness := range tt.reads {
				f.setMergeReadiness(t, readiness, "", syncNow.Add(time.Duration(i+1)*time.Minute))
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
			if tt.wantEvents != nil {
				assert.Equal(t, tt.wantEvents, told[0].Events, "the mail names the events the change started")
			}
		})
	}
}

// TestTheMailNamesTheForgesOwnTool pins that the mail uses the noun, the number and the checks
// command of the forge the pull request is on.
func TestTheMailNamesTheForgesOwnTool(t *testing.T) {
	tests := []struct {
		name      string
		forge     string
		wantLines []string
	}{
		{
			name:      "github",
			forge:     forge.KindGitHub,
			wantLines: []string{"komrad: pull request #7 (", "Read its checks with gh pr checks 7."},
		},
		{
			name:      "gitlab",
			forge:     forge.KindGitLab,
			wantLines: []string{"komrad: merge request !7 (", "Read its checks with glab ci status --branch agent/x."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := render(pullrequeststore.PullRequest{
				Forge:      tt.forge,
				Number:     7,
				HeadBranch: "agent/x",
			})
			for _, line := range tt.wantLines {
				assert.Contains(t, body, line)
			}
		})
	}
}

// TestAPullRequestChangeAfterTheMailWasReadStartsAnother pins that mail the agent read is left alone.
func TestAPullRequestChangeAfterTheMailWasReadStartsAnother(t *testing.T) {
	failingOnA := forge.MergeReadiness{
		Decision: forge.ReviewDecisionPending,
		Checks:   forge.ChecksFailing,
	}

	tests := []struct {
		name      string
		after     forge.MergeReadiness
		afterHead string
		wantLines []string
	}{
		{
			name: "an approval",
			after: forge.MergeReadiness{
				Decision: forge.ReviewDecisionApproved,
				Checks:   forge.ChecksFailing,
			},
			afterHead: "aaa",
			wantLines: []string{"  review: approved\n", "  checks: failing on aaa\n"},
		},
		{
			name: "the checks failing again on a new head",
			after: forge.MergeReadiness{
				Decision: forge.ReviewDecisionPending,
				Checks:   forge.ChecksFailing,
			},
			afterHead: "bbb",
			wantLines: []string{"  checks: failing on bbb\n"},
		},
		{
			name: "an approval on a new head with no checks yet leaves the checks unknown",
			after: forge.MergeReadiness{
				Decision: forge.ReviewDecisionApproved,
				Checks:   forge.ChecksNone,
			},
			afterHead: "bbb",
			wantLines: []string{"  review: approved\n", "  checks: unknown\n"},
		},
		{
			name:      "the checks failing again on the same head is not news",
			after:     failingOnA,
			afterHead: "aaa",
		},
		{
			name: "the checks passing on a new head is not news",
			after: forge.MergeReadiness{
				Decision: forge.ReviewDecisionPending,
				Checks:   forge.ChecksPassing,
			},
			afterHead: "bbb",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSyncFixture(t)
			agentID := f.agentOn(t, "a", "agent/x")
			f.hold(withMergeReadiness(pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Minute)), forge.MergeReadiness{
				Decision:     forge.ReviewDecisionPending,
				Checks:       forge.ChecksPending,
				Mergeability: forge.MergeabilityClean,
			}))
			f.sync(t)

			f.setMergeReadiness(t, failingOnA, "aaa", syncNow.Add(time.Minute))
			claimed, err := f.stores.Mail.Claim(t.Context(), agentID, 10)
			require.NoError(t, err)
			require.Len(t, claimed, 1)

			f.setMergeReadiness(t, tt.after, tt.afterHead, syncNow.Add(2*time.Minute))

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

// setMergeReadiness has the forge say this of PR_1, on a new head when one is named, and reads it then.
func (f *syncFixture) setMergeReadiness(t *testing.T, said forge.MergeReadiness, head string, readAt time.Time) {
	t.Helper()

	if said.Mergeability == forge.MergeabilityUnknown {
		said.Mergeability = forge.MergeabilityClean
	}
	held := f.forge.pullRequests["harrisoncramer/komrad"]
	held[0] = withMergeReadiness(held[0], said)
	if head != "" {
		held[0].HeadSHA = head
	}
	f.syncer.Wake()
	f.pass(t, readAt)
}

// TestAPullRequestChangeRetriesAFullMailboxAlone pins one agent's full mailbox leaving the others on the branch told and its own mail retried once there is room.
func TestAPullRequestChangeRetriesAFullMailboxAlone(t *testing.T) {
	f := newSyncFixture(t)
	told := f.agentOn(t, "a", "agent/x")
	full := f.agentOn(t, "b", "agent/x")
	f.hold(withMergeReadiness(pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Minute)), forge.MergeReadiness{
		Decision:     forge.ReviewDecisionPending,
		Checks:       forge.ChecksPending,
		Mergeability: forge.MergeabilityClean,
	}))
	f.sync(t)
	f.fillMailbox(t, full)

	f.setMergeReadiness(t, forge.MergeReadiness{Decision: forge.ReviewDecisionPending, Checks: forge.ChecksFailing}, "", syncNow.Add(time.Minute))

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
	assert.Contains(t, pullRequestMail[0].Body, "  checks: failing on abc\n")
}

func (f *syncFixture) fillMailbox(t *testing.T, agentID string) {
	t.Helper()

	mailer := mail.NewMailService(mail.NewMailServiceParams{
		Mail:      f.stores.Mail,
		Agents:    f.stores.Agents,
		Workflows: f.stores.Workflows,
		Config:    config.Mail{},
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
