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

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAPullRequestChangeTellsTheAgentsOnItsBranch pins what the one unread mail about a pull request says after each pass.
func TestAPullRequestChangeTellsTheAgentsOnItsBranch(t *testing.T) {
	pending := forge.MergeReadiness{
		Decision:     forge.ReviewDecisionPending,
		Listing:      listedStatusChecks(forge.ChecksPending),
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
			reads:      []forge.MergeReadiness{{Decision: forge.ReviewDecisionPending, Listing: listedStatusChecks(forge.ChecksFailing)}},
			wantLines:  []string{"  checks: failing on abc\n"},
			wantEvents: []string{eventprompts.PullRequestChecksFailing},
		},
		{
			name:       "checks failing while others still run",
			reads:      []forge.MergeReadiness{{Decision: forge.ReviewDecisionPending, Listing: listedStatusChecks(forge.ChecksFailingRunning)}},
			wantLines:  []string{"  checks: failing on abc, others still running\n"},
			wantEvents: []string{eventprompts.PullRequestChecksFailingRunning},
		},
		{
			name: "the run finishing before the agent reads folds the early word into the settled one",
			reads: []forge.MergeReadiness{
				{Decision: forge.ReviewDecisionPending, Listing: listedStatusChecks(forge.ChecksFailingRunning)},
				{Decision: forge.ReviewDecisionPending, Listing: listedStatusChecks(forge.ChecksFailing)},
			},
			wantLines:  []string{"  checks: failing on abc\n"},
			wantEvents: []string{eventprompts.PullRequestChecksFailing},
		},
		{
			name:      "an approval",
			reads:     []forge.MergeReadiness{{Decision: forge.ReviewDecisionApproved, Listing: listedStatusChecks(forge.ChecksPending)}},
			wantLines: []string{"  review: approved\n", "do not fetch them from the forge"},
		},
		{
			name: "checks passing while the forge blocks the merge is not news",
			reads: []forge.MergeReadiness{{
				Decision:     forge.ReviewDecisionPending,
				Listing:      listedStatusChecks(forge.ChecksPassing),
				Mergeability: forge.MergeabilityBlocked,
			}},
		},
		{
			name: "checks passing on a pull request the forge would merge",
			reads: []forge.MergeReadiness{{
				Decision:     forge.ReviewDecisionPending,
				Listing:      listedStatusChecks(forge.ChecksPassing),
				Mergeability: forge.MergeabilityClean,
			}},
			wantLines:  []string{"  checks: passing on abc\n", "  mergeability: clean\n"},
			wantEvents: []string{eventprompts.PullRequestChecksPassing},
		},
		{
			name: "two changes before the agent reads are one mail",
			reads: []forge.MergeReadiness{
				{Decision: forge.ReviewDecisionPending, Listing: listedStatusChecks(forge.ChecksFailing)},
				{
					Decision:     forge.ReviewDecisionPending,
					Listing:      listedStatusChecks(forge.ChecksFailing),
					Mergeability: forge.MergeabilityConflicting,
				},
			},
			wantLines: []string{"  checks: failing on abc\n", "  mergeability: conflicting with main\n"},
		},
		{
			name: "a later change rewrites the mail to where things stand",
			reads: []forge.MergeReadiness{
				{Decision: forge.ReviewDecisionPending, Listing: listedStatusChecks(forge.ChecksFailing)},
				{Decision: forge.ReviewDecisionPending, Listing: listedStatusChecks(forge.ChecksPassing)},
			},
			wantLines: []string{"  checks: passing on abc\n"},
		},
		{
			name: "auto-merge turned on",
			reads: []forge.MergeReadiness{{
				Decision:  forge.ReviewDecisionPending,
				Listing:   listedStatusChecks(forge.ChecksPending),
				AutoMerge: forge.AutoMerge{On: true},
			}},
			wantLines: []string{"  auto-merge: on, so whatever is on the branch merges once its checks and reviews pass\n"},
		},
		{
			name: "auto-merge turned off again rewrites the mail",
			reads: []forge.MergeReadiness{
				{
					Decision:  forge.ReviewDecisionPending,
					Listing:   listedStatusChecks(forge.ChecksPending),
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

// TestTheMailNamesEachFailingCheck pins the noun and number of the forge the pull request is on,
// and the failing checks that decide its head, each with its link, split by whether they are news to the agent.
func TestTheMailNamesEachFailingCheck(t *testing.T) {
	optionalFailed := listedStatusCheck("3", "docs", forge.StatusCheckStateCompleted, "failure")
	optionalFailed.Required = false
	unlinked := listedStatusCheck("4", "deploy", forge.StatusCheckStateCompleted, "timed_out")
	unlinked.URL = ""
	heldSince := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	before := heldSince.Add(-time.Minute)

	tests := []struct {
		name             string
		forge            string
		checks           forge.Checks
		headStatusChecks []forge.StatusCheck
		notedAt          map[string]time.Time
		heldSince        *time.Time
		wantLines        []string
		wantNotLines     []string
	}{
		{
			name:             "github",
			forge:            forge.KindGitHub,
			checks:           forge.ChecksPassing,
			headStatusChecks: listedStatusChecks(forge.ChecksPassing).StatusChecks,
			wantLines:        []string{"komrad: pull request #7 ("},
			wantNotLines:     []string{"New failures:", "Still failing:"},
		},
		{
			name:      "gitlab",
			forge:     forge.KindGitLab,
			checks:    forge.ChecksPending,
			wantLines: []string{"komrad: merge request !7 ("},
		},
		{
			name:             "a failing required check the agent was not told of is new with its link",
			forge:            forge.KindGitHub,
			checks:           forge.ChecksFailingRunning,
			headStatusChecks: append(listedStatusChecks(forge.ChecksFailingRunning).StatusChecks, optionalFailed),
			wantLines:        []string{"New failures:\n  test: https://github.com/harrisoncramer/komrad/runs/1\nReview comments"},
			wantNotLines:     []string{"docs", "lint:", "Still failing:"},
		},
		{
			name:             "a failing check without a link says so",
			forge:            forge.KindGitHub,
			checks:           forge.ChecksFailing,
			headStatusChecks: []forge.StatusCheck{unlinked},
			wantLines:        []string{"New failures:\n  deploy: no link\n"},
		},
		{
			name:             "a failure the agent was told of before the held mail is still failing",
			forge:            forge.KindGitHub,
			checks:           forge.ChecksFailing,
			headStatusChecks: []forge.StatusCheck{listedStatusCheck("1", "test", forge.StatusCheckStateCompleted, "failure"), unlinked},
			notedAt:          map[string]time.Time{"test": before},
			heldSince:        &heldSince,
			wantLines:        []string{"New failures:\n  deploy: no link\nStill failing:\n  test: https://github.com/harrisoncramer/komrad/runs/1\nReview comments"},
		},
		{
			name:             "a failure the held mail told of is new",
			forge:            forge.KindGitHub,
			checks:           forge.ChecksFailing,
			headStatusChecks: listedStatusChecks(forge.ChecksFailing).StatusChecks,
			notedAt:          map[string]time.Time{"test": heldSince},
			heldSince:        &heldSince,
			wantLines:        []string{"New failures:\n  test: "},
			wantNotLines:     []string{"Still failing:"},
		},
		{
			name:             "a failure the agent was told of with no mail held is still failing",
			forge:            forge.KindGitHub,
			checks:           forge.ChecksFailing,
			headStatusChecks: listedStatusChecks(forge.ChecksFailing).StatusChecks,
			notedAt:          map[string]time.Time{"test": before},
			wantLines:        []string{"Still failing:\n  test: "},
			wantNotLines:     []string{"New failures:"},
		},
		{
			name:             "no check is named while the checks are unknown",
			forge:            forge.KindGitHub,
			headStatusChecks: listedStatusChecks(forge.ChecksFailing).StatusChecks,
			wantNotLines:     []string{"New failures:", "Still failing:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pullRequest := pullrequeststore.PullRequest{
				Forge:      tt.forge,
				Number:     7,
				HeadBranch: "agent/x",
				HeadSHA:    scriptedHeadSHA,
				MergeReadiness: forge.RememberedMergeReadiness{
					Checks: forge.RememberedAnswer[forge.Checks]{Value: tt.checks, HeadSHA: scriptedHeadSHA},
				},
				HeadStatusChecks: tt.headStatusChecks,
			}
			failures := xslice.Map(failedChecks(pullRequest), func(check forge.StatusCheck) pullrequeststore.CheckFailure {
				failure := pullrequeststore.CheckFailure{Check: check}
				if notedAt, ok := tt.notedAt[check.Name]; ok {
					failure.NotedAt = &notedAt
				}

				return failure
			})

			body := eventprompts.PullRequest{
				PullRequest: pullRequest,
				Fields:      listFields(pullRequest),
				Failures:    failures,
				HeldSince:   tt.heldSince,
			}.Body()
			for _, line := range tt.wantLines {
				assert.Contains(t, body, line)
			}
			for _, line := range tt.wantNotLines {
				assert.NotContains(t, body, line)
			}
		})
	}
}

// TestAPullRequestChangeAfterTheMailWasReadStartsAnother pins that mail the agent read is left alone.
func TestAPullRequestChangeAfterTheMailWasReadStartsAnother(t *testing.T) {
	failingOnA := forge.MergeReadiness{
		Decision: forge.ReviewDecisionPending,
		Listing:  listedStatusChecks(forge.ChecksFailing),
	}

	tests := []struct {
		name       string
		before     forge.MergeReadiness
		after      forge.MergeReadiness
		afterHead  string
		wantLines  []string
		wantEvents []string
	}{
		{
			name:   "an approval",
			before: failingOnA,
			after: forge.MergeReadiness{
				Decision: forge.ReviewDecisionApproved,
				Listing:  listedStatusChecks(forge.ChecksFailing),
			},
			afterHead: "aaa",
			wantLines: []string{"  review: approved\n", "  checks: failing on aaa\n"},
		},
		{
			name:   "the checks failing again on a new head",
			before: failingOnA,
			after: forge.MergeReadiness{
				Decision: forge.ReviewDecisionPending,
				Listing:  listedStatusChecks(forge.ChecksFailing),
			},
			afterHead: "bbb",
			wantLines: []string{"  checks: failing on bbb\n"},
		},
		{
			name:   "an approval on a new head with no checks yet leaves the checks unknown",
			before: failingOnA,
			after: forge.MergeReadiness{
				Decision: forge.ReviewDecisionApproved,
				Listing:  listedStatusChecks(forge.ChecksNone),
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
				Decision: forge.ReviewDecisionPending,
				Listing: &forge.StatusCheckListing{StatusChecks: []forge.StatusCheck{
					listedStatusCheck("1", "test", forge.StatusCheckStateCompleted, "failure"),
					listedStatusCheck("5", "build", forge.StatusCheckStateCompleted, "failure"),
				}},
			},
			afterHead: "aaa",
			wantLines: []string{
				"New failures:\n  build: https://github.com/harrisoncramer/komrad/runs/5\n",
				"Still failing:\n  test: https://github.com/harrisoncramer/komrad/runs/1\n",
			},
			wantEvents: []string{eventprompts.PullRequestChecksFailing},
		},
		{
			name:   "a rerun of the failed check failing again",
			before: failingOnA,
			after: forge.MergeReadiness{
				Decision: forge.ReviewDecisionPending,
				Listing: &forge.StatusCheckListing{StatusChecks: []forge.StatusCheck{
					listedStatusCheck("6", "test", forge.StatusCheckStateCompleted, "failure"),
				}},
			},
			afterHead:  "aaa",
			wantLines:  []string{"New failures:\n  test: https://github.com/harrisoncramer/komrad/runs/6\n"},
			wantEvents: []string{eventprompts.PullRequestChecksFailing},
		},
		{
			name: "the run finishing with no new failure after the early word was read",
			before: forge.MergeReadiness{
				Decision: forge.ReviewDecisionPending,
				Listing:  listedStatusChecks(forge.ChecksFailingRunning),
			},
			after: forge.MergeReadiness{
				Decision: forge.ReviewDecisionPending,
				Listing: &forge.StatusCheckListing{StatusChecks: []forge.StatusCheck{
					listedStatusCheck("1", "test", forge.StatusCheckStateCompleted, "failure"),
					listedStatusCheck("2", "lint", forge.StatusCheckStateCompleted, "success"),
				}},
			},
			afterHead:  "aaa",
			wantLines:  []string{"  checks: failing on aaa\n", "Still failing:\n  test: https://github.com/harrisoncramer/komrad/runs/1\n"},
			wantEvents: []string{eventprompts.PullRequestChecksFailing},
		},
		{
			name:   "the checks passing on a new head the forge blocks is not news",
			before: failingOnA,
			after: forge.MergeReadiness{
				Decision:     forge.ReviewDecisionPending,
				Listing:      listedStatusChecks(forge.ChecksPassing),
				Mergeability: forge.MergeabilityBlocked,
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
				Listing:      listedStatusChecks(forge.ChecksPending),
				Mergeability: forge.MergeabilityClean,
			}))
			f.sync(t)

			f.setMergeReadiness(t, tt.before, "aaa", syncNow.Add(time.Minute))
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
			if tt.wantEvents != nil {
				assert.Equal(t, tt.wantEvents, told[1].Events)
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
		Listing:      listedStatusChecks(forge.ChecksPending),
		Mergeability: forge.MergeabilityClean,
	}))
	f.sync(t)
	f.fillMailbox(t, full)

	f.setMergeReadiness(t, forge.MergeReadiness{Decision: forge.ReviewDecisionPending, Listing: listedStatusChecks(forge.ChecksFailing)}, "", syncNow.Add(time.Minute))

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
	assert.Equal(t, []string{eventprompts.PullRequestChecksFailing}, pullRequestMail[0].Events, "the full mailbox noted nothing, so the retry still tells of the failure")
}

// TestAMutedFailureIsToldOnceUnmuted pins that a failure muted out of the mail is still news to the agent once
// the workflow hears of failures again.
func TestAMutedFailureIsToldOnceUnmuted(t *testing.T) {
	f := newSyncFixture(t)
	agentID := f.agentOn(t, "a", "agent/x")
	f.setEventPrompts(t, []workflowstore.EventPrompt{{Event: eventprompts.PullRequestChecksFailing, Muted: true}})
	f.hold(withMergeReadiness(pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Minute)), forge.MergeReadiness{
		Decision:     forge.ReviewDecisionPending,
		Listing:      listedStatusChecks(forge.ChecksPending),
		Mergeability: forge.MergeabilityClean,
	}))
	f.sync(t)

	f.setMergeReadiness(t, forge.MergeReadiness{Decision: forge.ReviewDecisionPending, Listing: listedStatusChecks(forge.ChecksFailing)}, "aaa", syncNow.Add(time.Minute))
	require.Empty(t, f.listMail(t, agentID), "a muted failure opens no mail")

	f.setEventPrompts(t, nil)
	f.setMergeReadiness(t, forge.MergeReadiness{Decision: forge.ReviewDecisionApproved, Listing: listedStatusChecks(forge.ChecksFailing)}, "aaa", syncNow.Add(2*time.Minute))

	told := f.listMail(t, agentID)
	require.Len(t, told, 1)
	assert.Equal(t, []string{eventprompts.PullRequestApproved, eventprompts.PullRequestChecksFailing}, told[0].Events)
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
			Mail:      eventprompts.ThreadAnswered{WriterName: "waiting"},
			SubjectID: uuid.NewString(),
		})
		if errors.Is(err, mail.ErrMailboxFull) {
			return
		}
		require.NoError(t, err)
	}
}
