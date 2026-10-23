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
	workflowstore "komrad/internal/store/workflow"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	eventv1 "komrad/proto/gen/event/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAPullRequestChangeTellsTheAgentsOnItsBranch pins what the one unread mail about a pull request says after each pass.
func TestAPullRequestChangeTellsTheAgentsOnItsBranch(t *testing.T) {
	pending := forge.MergeReadiness{
		Decision:     pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING,
		Listing:      listedStatusChecks(pullrequestv1.Checks_CHECKS_PENDING),
		Mergeability: pullrequestv1.Mergeability_MERGEABILITY_CLEAN,
	}

	tests := []struct {
		name       string
		reads      []forge.MergeReadiness
		wantLines  []string
		wantEvents []eventv1.Event
	}{
		{
			name:       "checks failing",
			reads:      []forge.MergeReadiness{{Decision: pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING, Listing: listedStatusChecks(pullrequestv1.Checks_CHECKS_FAILING)}},
			wantLines:  []string{"  checks: failing on abc\n"},
			wantEvents: []eventv1.Event{eventv1.Event_EVENT_PULL_REQUEST_CHECKS_FAILING},
		},
		{
			name:       "checks failing while others still run",
			reads:      []forge.MergeReadiness{{Decision: pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING, Listing: listedStatusChecks(pullrequestv1.Checks_CHECKS_FAILING_RUNNING)}},
			wantLines:  []string{"  checks: failing on abc, others still running\n"},
			wantEvents: []eventv1.Event{eventv1.Event_EVENT_PULL_REQUEST_CHECKS_FAILING_RUNNING},
		},
		{
			name: "the run finishing before the agent reads folds the early word into the settled one",
			reads: []forge.MergeReadiness{
				{Decision: pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING, Listing: listedStatusChecks(pullrequestv1.Checks_CHECKS_FAILING_RUNNING)},
				{Decision: pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING, Listing: listedStatusChecks(pullrequestv1.Checks_CHECKS_FAILING)},
			},
			wantLines:  []string{"  checks: failing on abc\n"},
			wantEvents: []eventv1.Event{eventv1.Event_EVENT_PULL_REQUEST_CHECKS_FAILING},
		},
		{
			name:      "an approval",
			reads:     []forge.MergeReadiness{{Decision: pullrequestv1.ReviewDecision_REVIEW_DECISION_APPROVED, Listing: listedStatusChecks(pullrequestv1.Checks_CHECKS_PENDING)}},
			wantLines: []string{"  review: approved\n", "do not fetch them from the forge"},
		},
		{
			name: "checks passing while the forge blocks the merge is not news",
			reads: []forge.MergeReadiness{{
				Decision:     pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING,
				Listing:      listedStatusChecks(pullrequestv1.Checks_CHECKS_PASSING),
				Mergeability: pullrequestv1.Mergeability_MERGEABILITY_BLOCKED,
			}},
		},
		{
			name: "checks passing on a pull request the forge would merge",
			reads: []forge.MergeReadiness{{
				Decision:     pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING,
				Listing:      listedStatusChecks(pullrequestv1.Checks_CHECKS_PASSING),
				Mergeability: pullrequestv1.Mergeability_MERGEABILITY_CLEAN,
			}},
			wantLines:  []string{"  checks: passing on abc\n", "  mergeability: clean\n"},
			wantEvents: []eventv1.Event{eventv1.Event_EVENT_PULL_REQUEST_CHECKS_PASSING},
		},
		{
			name: "two changes before the agent reads are one mail",
			reads: []forge.MergeReadiness{
				{Decision: pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING, Listing: listedStatusChecks(pullrequestv1.Checks_CHECKS_FAILING)},
				{
					Decision:     pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING,
					Listing:      listedStatusChecks(pullrequestv1.Checks_CHECKS_FAILING),
					Mergeability: pullrequestv1.Mergeability_MERGEABILITY_CONFLICTING,
				},
			},
			wantLines: []string{"  checks: failing on abc\n", "  mergeability: conflicting with main\n"},
		},
		{
			name: "a later change rewrites the mail to where things stand",
			reads: []forge.MergeReadiness{
				{Decision: pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING, Listing: listedStatusChecks(pullrequestv1.Checks_CHECKS_FAILING)},
				{Decision: pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING, Listing: listedStatusChecks(pullrequestv1.Checks_CHECKS_PASSING)},
			},
			wantLines: []string{"  checks: passing on abc\n"},
		},
		{
			name: "auto-merge turned on",
			reads: []forge.MergeReadiness{{
				Decision:  pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING,
				Listing:   listedStatusChecks(pullrequestv1.Checks_CHECKS_PENDING),
				AutoMerge: forge.AutoMerge{On: true},
			}},
			wantLines: []string{"  auto-merge: on, so whatever is on the branch merges once its checks and reviews pass\n"},
		},
		{
			name: "auto-merge turned off again rewrites the mail",
			reads: []forge.MergeReadiness{
				{
					Decision:  pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING,
					Listing:   listedStatusChecks(pullrequestv1.Checks_CHECKS_PENDING),
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
			assert.Equal(t, agentv1.MailKind_MAIL_KIND_PULL_REQUEST_CHANGED, told[0].Kind)
			for _, line := range tt.wantLines {
				assert.Contains(t, told[0].Body, line)
			}
			if tt.wantEvents != nil {
				assert.Equal(t, tt.wantEvents, told[0].Events, "the mail names the events the change started")
			}
		})
	}
}

// TestTheMailNamesOnlyTheRequiredChecksFailingOnTheHead pins which checks the pull request mail lists as failing.
func TestTheMailNamesOnlyTheRequiredChecksFailingOnTheHead(t *testing.T) {
	optionalFailed := listedStatusCheck("3", "docs", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, "failure")
	optionalFailed.Required = false

	tests := []struct {
		name             string
		checks           pullrequestv1.Checks
		headStatusChecks []forge.StatusCheck
		want             []string
	}{
		{
			name:             "a required failure beside an optional failure and a running check",
			checks:           pullrequestv1.Checks_CHECKS_FAILING_RUNNING,
			headStatusChecks: append(listedStatusChecks(pullrequestv1.Checks_CHECKS_FAILING_RUNNING).StatusChecks, optionalFailed),
			want:             []string{"test"},
		},
		{
			name:             "no check is named while the checks are unknown",
			headStatusChecks: listedStatusChecks(pullrequestv1.Checks_CHECKS_FAILING).StatusChecks,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pullRequest := pullrequeststore.PullRequest{
				HeadSHA: scriptedHeadSHA,
				MergeReadiness: forge.RememberedMergeReadiness{
					Checks: forge.RememberedAnswer[pullrequestv1.Checks]{Value: tt.checks, HeadSHA: scriptedHeadSHA},
				},
				HeadStatusChecks: tt.headStatusChecks,
			}

			got := xslice.Map(failedChecks(pullRequest), func(check forge.StatusCheck) string { return check.Name })
			assert.ElementsMatch(t, tt.want, got)
		})
	}
}

// TestAPullRequestChangeAfterTheMailWasReadStartsAnother pins that mail the agent read is left alone.
func TestAPullRequestChangeAfterTheMailWasReadStartsAnother(t *testing.T) {
	failingOnA := forge.MergeReadiness{
		Decision: pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING,
		Listing:  listedStatusChecks(pullrequestv1.Checks_CHECKS_FAILING),
	}

	tests := []struct {
		name       string
		before     forge.MergeReadiness
		after      forge.MergeReadiness
		afterHead  string
		wantLines  []string
		wantEvents []eventv1.Event
	}{
		{
			name:   "an approval",
			before: failingOnA,
			after: forge.MergeReadiness{
				Decision: pullrequestv1.ReviewDecision_REVIEW_DECISION_APPROVED,
				Listing:  listedStatusChecks(pullrequestv1.Checks_CHECKS_FAILING),
			},
			afterHead: "aaa",
			wantLines: []string{"  review: approved\n", "  checks: failing on aaa\n"},
		},
		{
			name:   "the checks failing again on a new head",
			before: failingOnA,
			after: forge.MergeReadiness{
				Decision: pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING,
				Listing:  listedStatusChecks(pullrequestv1.Checks_CHECKS_FAILING),
			},
			afterHead: "bbb",
			wantLines: []string{"  checks: failing on bbb\n"},
		},
		{
			name:   "an approval on a new head with no checks yet leaves the checks unknown",
			before: failingOnA,
			after: forge.MergeReadiness{
				Decision: pullrequestv1.ReviewDecision_REVIEW_DECISION_APPROVED,
				Listing:  listedStatusChecks(pullrequestv1.Checks_CHECKS_NONE),
			},
			afterHead: "bbb",
			wantLines: []string{"  review: approved\n", "  checks: unknown\n"},
		},
		{
			name:      "the same failure on the same head is not news",
			before:    failingOnA,
			after:     failingOnA,
			afterHead: "aaa",
		},
		{
			name:   "another check failing on the same head",
			before: failingOnA,
			after: forge.MergeReadiness{
				Decision: pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING,
				Listing: &forge.StatusCheckListing{StatusChecks: []forge.StatusCheck{
					listedStatusCheck("1", "test", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, "failure"),
					listedStatusCheck("5", "build", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, "failure"),
				}},
			},
			afterHead: "aaa",
			wantLines: []string{
				"New failures:\n  build: https://github.com/harrisoncramer/komrad/runs/5\n",
				"Still failing:\n  test: https://github.com/harrisoncramer/komrad/runs/1\n",
			},
			wantEvents: []eventv1.Event{eventv1.Event_EVENT_PULL_REQUEST_CHECKS_FAILING},
		},
		{
			name:   "a rerun of the failed check failing again",
			before: failingOnA,
			after: forge.MergeReadiness{
				Decision: pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING,
				Listing: &forge.StatusCheckListing{StatusChecks: []forge.StatusCheck{
					listedStatusCheck("6", "test", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, "failure"),
				}},
			},
			afterHead:  "aaa",
			wantLines:  []string{"New failures:\n  test: https://github.com/harrisoncramer/komrad/runs/6\n"},
			wantEvents: []eventv1.Event{eventv1.Event_EVENT_PULL_REQUEST_CHECKS_FAILING},
		},
		{
			name: "the run finishing with no new failure after the early word was read",
			before: forge.MergeReadiness{
				Decision: pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING,
				Listing:  listedStatusChecks(pullrequestv1.Checks_CHECKS_FAILING_RUNNING),
			},
			after: forge.MergeReadiness{
				Decision: pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING,
				Listing: &forge.StatusCheckListing{StatusChecks: []forge.StatusCheck{
					listedStatusCheck("1", "test", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, "failure"),
					listedStatusCheck("2", "lint", pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED, "success"),
				}},
			},
			afterHead:  "aaa",
			wantLines:  []string{"  checks: failing on aaa\n", "Still failing:\n  test: https://github.com/harrisoncramer/komrad/runs/1\n"},
			wantEvents: []eventv1.Event{eventv1.Event_EVENT_PULL_REQUEST_CHECKS_FAILING},
		},
		{
			name:   "the checks passing on a new head the forge blocks is not news",
			before: failingOnA,
			after: forge.MergeReadiness{
				Decision:     pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING,
				Listing:      listedStatusChecks(pullrequestv1.Checks_CHECKS_PASSING),
				Mergeability: pullrequestv1.Mergeability_MERGEABILITY_BLOCKED,
			},
			afterHead: "bbb",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSyncFixture(t)
			agentID := f.agentOn(t, "a", "agent/x")
			f.hold(withMergeReadiness(pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Minute)), forge.MergeReadiness{
				Decision:     pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING,
				Listing:      listedStatusChecks(pullrequestv1.Checks_CHECKS_PENDING),
				Mergeability: pullrequestv1.Mergeability_MERGEABILITY_CLEAN,
			}))
			f.sync(t)

			f.setMergeReadiness(t, tt.before, "aaa", syncNow.Add(time.Minute))
			claimed, err := f.stores.ClaimMail(t.Context(), agentID, 10)
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
			if tt.wantEvents != nil {
				assert.Equal(t, tt.wantEvents, told[1].Events)
			}
		})
	}
}

func (f *syncFixture) listMail(t *testing.T, agentID uuid.UUID) []mailstore.Mail {
	t.Helper()

	listed, err := f.stores.Mail.ListPage(t.Context(), agentID, 200, nil)
	require.NoError(t, err)

	slices.Reverse(listed)

	return listed
}

// setMergeReadiness has the forge say this of PR_1, on a new head when one is named, and reads it then.
func (f *syncFixture) setMergeReadiness(t *testing.T, said forge.MergeReadiness, head string, readAt time.Time) {
	t.Helper()

	if said.Mergeability == pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN {
		said.Mergeability = pullrequestv1.Mergeability_MERGEABILITY_CLEAN
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
		Decision:     pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING,
		Listing:      listedStatusChecks(pullrequestv1.Checks_CHECKS_PENDING),
		Mergeability: pullrequestv1.Mergeability_MERGEABILITY_CLEAN,
	}))
	f.sync(t)
	f.fillMailbox(t, full)

	f.setMergeReadiness(t, forge.MergeReadiness{Decision: pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING, Listing: listedStatusChecks(pullrequestv1.Checks_CHECKS_FAILING)}, "", syncNow.Add(time.Minute))

	assert.Empty(t, f.events.TakeErrors())
	require.Len(t, f.listMail(t, told), 1)
	followUps := f.events.ListFollowUps()
	require.Len(t, followUps, 1)
	retry, ok := followUps[0].(events.Event)
	require.True(t, ok)
	var change Change
	require.NoError(t, json.Unmarshal(retry.Payload, &change))
	assert.Equal(t, &full, change.Recipient)

	_, err := f.stores.ClaimMail(t.Context(), full, 200)
	require.NoError(t, err)
	_, err = f.stores.ClaimMail(t.Context(), told, 10)
	require.NoError(t, err)
	require.NoError(t, f.events.InsertTx(t.Context(), retry))

	assert.Empty(t, f.events.TakeErrors())
	require.Len(t, f.listMail(t, told), 1, "the retry tells only the agent it was for")
	var pullRequestMail []mailstore.Mail
	for _, held := range f.listMail(t, full) {
		if held.Kind == agentv1.MailKind_MAIL_KIND_PULL_REQUEST_CHANGED {
			pullRequestMail = append(pullRequestMail, held)
		}
	}
	require.Len(t, pullRequestMail, 1)
	assert.Contains(t, pullRequestMail[0].Body, "  checks: failing on abc\n")
	assert.Equal(t, []eventv1.Event{eventv1.Event_EVENT_PULL_REQUEST_CHECKS_FAILING}, pullRequestMail[0].Events, "the full mailbox noted nothing, so the retry still tells of the failure")
}

// TestAMutedFailureIsToldOnceUnmuted pins that a failure muted out of the mail is still news to the agent once
// the workflow hears of failures again.
func TestAMutedFailureIsToldOnceUnmuted(t *testing.T) {
	f := newSyncFixture(t)
	agentID := f.agentOn(t, "a", "agent/x")
	f.setEventPrompts(t, []workflowstore.EventPrompt{{Event: eventv1.Event_EVENT_PULL_REQUEST_CHECKS_FAILING, Muted: true}})
	f.hold(withMergeReadiness(pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Minute)), forge.MergeReadiness{
		Decision:     pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING,
		Listing:      listedStatusChecks(pullrequestv1.Checks_CHECKS_PENDING),
		Mergeability: pullrequestv1.Mergeability_MERGEABILITY_CLEAN,
	}))
	f.sync(t)

	f.setMergeReadiness(t, forge.MergeReadiness{Decision: pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING, Listing: listedStatusChecks(pullrequestv1.Checks_CHECKS_FAILING)}, "aaa", syncNow.Add(time.Minute))
	require.Empty(t, f.listMail(t, agentID), "a muted failure opens no mail")

	f.setEventPrompts(t, nil)
	f.setMergeReadiness(t, forge.MergeReadiness{Decision: pullrequestv1.ReviewDecision_REVIEW_DECISION_APPROVED, Listing: listedStatusChecks(pullrequestv1.Checks_CHECKS_FAILING)}, "aaa", syncNow.Add(2*time.Minute))

	told := f.listMail(t, agentID)
	require.Len(t, told, 1)
	assert.Equal(t, []eventv1.Event{eventv1.Event_EVENT_PULL_REQUEST_APPROVED, eventv1.Event_EVENT_PULL_REQUEST_CHECKS_FAILING}, told[0].Events)
	assert.Contains(t, told[0].Body, "New failures:\n  test: https://github.com/harrisoncramer/komrad/runs/1\n")
}

func (f *syncFixture) setEventPrompts(t *testing.T, prompts []workflowstore.EventPrompt) {
	t.Helper()

	workflow, err := f.stores.Workflows.Get(t.Context(), f.workflow)
	require.NoError(t, err)
	cfg := workflow.Config()
	cfg.EventPrompts = prompts
	require.NoError(t, f.stores.Workflows.Update(t.Context(), workflow.ID, cfg))
}

func (f *syncFixture) fillMailbox(t *testing.T, agentID uuid.UUID) {
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
			Mail:      eventprompts.ThreadAnswered{WriterName: "waiting"},
			SubjectID: uuid.New(),
		})
		if errors.Is(err, mail.ErrMailboxFull) {
			return
		}
		require.NoError(t, err)
	}
}
