package ui_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	forgev1 "komrad/proto/gen/forge/v1"
	"komrad/proto/gen/forge/v1/forgev1connect"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	taskv1 "komrad/proto/gen/task/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestGitMenu covers the menu of git actions rendered over the agent the cursor is on.
func TestGitMenu(t *testing.T) {
	t.Parallel()

	t.Run("the menu opens it to its own right and names both in the trail", func(t *testing.T) {
		t.Parallel()

		harness := openGitMenu(t, buildGitAgent()...)

		frame := harness.readFrame()
		assert.Equal(t, "agents › api › git", readTrail(frame))
		assert.Contains(t, frame, "  › git ›", "the menu it came from stays rendered with its cursor on git")
	})

	t.Run("esc goes back to the menu it came from, its cursor still on git", func(t *testing.T) {
		t.Parallel()

		harness := openGitMenu(t, buildGitAgent()...)

		harness.press("esc")

		frame := harness.readFrame()
		assert.NotContains(t, frame, gitMenuEntry)
		assert.Contains(t, frame, "› git ›")
		assert.Equal(t, "agents › api", readTrail(frame))

		harness.press("esc")

		frame = harness.readFrame()
		assert.NotContains(t, frame, "git ›")
		assert.NotContains(t, frame, "Could not")
	})

	t.Run("back does what esc does", func(t *testing.T) {
		t.Parallel()

		harness := openGitMenu(t, buildGitAgent()...)

		harness.press("k", "enter")

		frame := harness.readFrame()
		assert.NotContains(t, frame, gitMenuEntry)
		assert.Contains(t, frame, "› git ›")
	})

	t.Run("a narrow panel renders only the menu on top", func(t *testing.T) {
		t.Parallel()

		harness := newPanelHarness(t, panelHarnessParams{width: 36, height: 20})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(buildGitAgent()...))
		harness.press("space", "g")

		frame := harness.readFrame()
		assert.Contains(t, frame, gitMenuEntry)
		assert.NotContains(t, frame, "documents")
	})

	t.Run("an entry answers its own letter", func(t *testing.T) {
		t.Parallel()

		harness := openGitMenu(t, buildGitAgent()...)

		harness.press("r")

		assert.NotContains(t, harness.readFrame(), gitMenuEntry)
		harness.assertSentTo(agentv1connect.AgentServicePlanRebaseProcedure, &agentv1.PlanRebaseRequest{AgentId: "a1"})
	})

	t.Run("a merge request's entries are named the way GitLab does", func(t *testing.T) {
		t.Parallel()

		harness := openGitMenu(t, append(buildGitAgent(), onGitLab(newOpenPullRequest()))...)

		frame := harness.readFrame()
		assert.Contains(t, frame, "open mr")
		assert.Contains(t, frame, "copy mr url")
		assert.Contains(t, frame, "merge mr")
		assert.NotContains(t, frame, "open pr")
	})

	t.Run("the menu takes no filter, so the letters stay the entries'", func(t *testing.T) {
		t.Parallel()

		harness := openGitMenu(t, buildGitAgent()...)
		require.NotContains(t, harness.readFrame(), "filter")

		harness.press("/")

		assert.Contains(t, harness.readFrame(), "rebase with base")
	})

	t.Run("an entry picked after its agent has left the list says so", func(t *testing.T) {
		t.Parallel()

		harness := openGitMenu(t, buildGitAgent()...)
		harness.watch(buildSnapshot(buildGitAgent()[0]))

		harness.press("r")

		assert.Contains(t, readOpenMessages(harness), "the agent the menu was opened on has left the list")
		harness.assertSentTo(agentv1connect.AgentServicePlanRebaseProcedure)
	})
}

// TestGitMenuRefusals covers an entry that would do nothing saying why when picked.
func TestGitMenuRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		rows        []proto.Message
		press       []string
		wantRefusal string
	}{
		{
			name:        "the rebase of an agent with no worktree of its own",
			rows:        buildUncutAgent(),
			press:       []string{"r"},
			wantRefusal: "this agent has no worktree of its own for komrad to act on",
		},
		{
			name:        "the push of an agent with no worktree of its own",
			rows:        buildUncutAgent(),
			press:       []string{"p"},
			wantRefusal: "this agent has no worktree of its own for komrad to act on",
		},
		{
			name:        "the push of a worktree part-way through a rebase",
			rows:        buildRebasingGitAgent(),
			press:       []string{"p"},
			wantRefusal: "part-way through a rebase, merge or pick",
		},
		{
			name:        "the abort of a worktree with nothing in progress",
			rows:        buildGitAgent(),
			press:       []string{"x"},
			wantRefusal: "nothing is part-way through in this worktree",
		},
		{
			name:        "the pull request of a repository not read yet",
			rows:        buildUncutAgent(),
			press:       []string{"o"},
			wantRefusal: "komrad has not read this agent's repository yet",
		},
		{
			name:        "the pull request of a branch with none",
			rows:        append(buildUncutAgent(), &pullrequestv1.Repository{Url: repositoryURL}),
			press:       []string{"o"},
			wantRefusal: "komrad has read the repository and nothing has been opened from this branch",
		},
		{
			name:        "the pull request of a branch that has moved past its last one",
			rows:        append(buildUncutAgent(), newMergedPullRequest()),
			press:       []string{"o"},
			wantRefusal: "the branch has moved on since its last pull request finished",
		},
		{
			name:        "the copy of a pull request address on a branch with none",
			rows:        buildUncutAgent(),
			press:       []string{"c"},
			wantRefusal: "komrad has not read this agent's repository yet",
		},
		{
			name:        "the merge of a branch with no pull request",
			rows:        buildUncutAgent(),
			press:       []string{"m", "m"},
			wantRefusal: "komrad has not read this agent's repository yet",
		},
		{
			name:        "the merge of a pull request that has already merged",
			rows:        append(buildGitAgent(), newCurrentMergedPullRequest()),
			press:       []string{"m", "m"},
			wantRefusal: "its pull request has already merged",
		},
		{
			name:        "the merge when ready of a draft",
			rows:        append(buildGitAgent(), newQueueableDraft()),
			press:       []string{"m", "u"},
			wantRefusal: "its pull request is a draft, so mark it ready first",
		},
		{
			name:        "the merge when ready of a pull request the forge will not queue",
			rows:        append(buildGitAgent(), newOpenPullRequest()),
			press:       []string{"m", "u"},
			wantRefusal: "the forge will not queue it now",
		},
		{
			name:        "the merge then rebase of a worktree part-way through a rebase",
			rows:        append(buildRebasingGitAgent(), newOpenPullRequest()),
			press:       []string{"m", "r"},
			wantRefusal: "part-way through a rebase, merge or pick",
		},
		{
			name:        "the merge then rebase of a branch with no pull request",
			rows:        buildGitAgent(),
			press:       []string{"m", "r"},
			wantRefusal: "komrad has not read this agent's repository yet",
		},
		{
			name:        "the draft toggle of a pull request that has already merged",
			rows:        append(buildGitAgent(), newCurrentMergedPullRequest()),
			press:       []string{"d"},
			wantRefusal: "its pull request has already merged",
		},
		{
			name:        "the merge of a merge request that has already merged",
			rows:        append(buildGitAgent(), onGitLab(newCurrentMergedPullRequest())),
			press:       []string{"m", "m"},
			wantRefusal: "its merge request has already merged",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openGitMenu(t, tt.rows...)

			harness.press(tt.press...)

			assert.NotContains(t, harness.readFrame(), gitMenuEntry)
			assert.Contains(t, readOpenMessages(harness), tt.wantRefusal)
		})
	}
}

// TestGitMenuMerges covers the merge menu merging the current pull request and archiving the
// agent after.
func TestGitMenuMerges(t *testing.T) {
	t.Parallel()

	merge := &forgev1.MergeForgePullRequestRequest{Url: pullRequestURL}

	tests := []struct {
		name        string
		forge       string
		press       []string
		refuse      error
		wantMerges  []proto.Message
		wantArchive []proto.Message
		wantMessage string
	}{
		{
			name:        "merge leaves the agent where it is",
			press:       []string{"m"},
			wantMerges:  []proto.Message{merge},
			wantMessage: "api: merged #740",
		},
		{
			name:        "merge names a merge request the way GitLab does",
			forge:       "gitlab",
			press:       []string{"m"},
			wantMerges:  []proto.Message{merge},
			wantMessage: "api: merged !740",
		},
		{
			name:        "merge and archive archives the agent once the forge has merged",
			press:       []string{"a"},
			wantMerges:  []proto.Message{merge},
			wantArchive: []proto.Message{&agentv1.ArchiveAgentsRequest{Ids: []string{"a1"}}},
			wantMessage: "api: merged #740",
		},
		{
			name:  "back sends nothing",
			press: []string{"esc"},
		},
		{
			name:        "a merge the forge refuses archives nothing",
			press:       []string{"a"},
			refuse:      connect.NewError(connect.CodeFailedPrecondition, errors.New("github refused to merge it: Pull Request is not mergeable")),
			wantMerges:  []proto.Message{merge},
			wantMessage: "Pull Request is not mergeable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pullRequest := newOpenPullRequest()
			pullRequest.Forge = tt.forge
			harness := openGitMenu(t, append(buildGitAgent(), pullRequest)...)
			if tt.refuse != nil {
				harness.refuse(forgev1connect.ForgeServiceMergeForgePullRequestProcedure, tt.refuse)
			}

			harness.press("m")
			require.Equal(t, "agents › api › git › merge", readTrail(harness.readFrame()))
			harness.press(tt.press...)

			harness.assertSentTo(forgev1connect.ForgeServiceMergeForgePullRequestProcedure, tt.wantMerges...)
			harness.assertSentTo(agentv1connect.AgentServiceArchiveAgentsProcedure, tt.wantArchive...)
			assert.NotContains(t, harness.readFrame(), "merge and archive")
			if tt.wantMessage != "" {
				assert.Contains(t, readOpenMessages(harness), tt.wantMessage)
			}
		})
	}
}

// TestGitMenuMergesThenRebases covers the entry that merges a pull request on the forge and then
// brings its worktree up to date on the base, rebasing nothing when the forge refuses.
func TestGitMenuMergesThenRebases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		refuse       error
		wantRebases  []proto.Message
		wantMessages []string
	}{
		{
			name:         "a merged pull request has its worktree moved onto the base",
			wantRebases:  []proto.Message{&agentv1.RebaseWorktreeRequest{AgentId: "a1"}},
			wantMessages: []string{"api: merged #740", "api: moved onto main"},
		},
		{
			name:         "a merge the forge refuses rebases nothing",
			refuse:       connect.NewError(connect.CodeFailedPrecondition, errors.New("github refused to merge it: Pull Request is not mergeable")),
			wantMessages: []string{"Pull Request is not mergeable"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openGitMenu(t, append(buildGitAgent(), newOpenPullRequest())...)
			if tt.refuse != nil {
				harness.refuse(forgev1connect.ForgeServiceMergeForgePullRequestProcedure, tt.refuse)
			}
			harness.answer(agentv1connect.AgentServiceRebaseWorktreeProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
				return connect.NewResponse(&agentv1.RebaseWorktreeResponse{
					BaseRef: "main",
					BaseSha: "abcdef1234567",
					Moved:   true,
				}), nil
			})

			harness.press("m", "r")

			harness.assertSentTo(forgev1connect.ForgeServiceMergeForgePullRequestProcedure, &forgev1.MergeForgePullRequestRequest{Url: pullRequestURL})
			harness.assertSentTo(agentv1connect.AgentServiceRebaseWorktreeProcedure, tt.wantRebases...)
			harness.assertSentTo(agentv1connect.AgentServicePlanRebaseProcedure)
			assert.NotContains(t, harness.readFrame(), "merge then rebase")
			messages := readOpenMessages(harness)
			for _, want := range tt.wantMessages {
				assert.Contains(t, messages, want)
			}
		})
	}
}

// TestGitMenuMergesWhenReady covers the merge menu entry naming which way it goes, asking the
// forge to merge on its own at the head the panel holds, and stopping it again.
func TestGitMenuMergesWhenReady(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		pullRequest *pullrequestv1.PullRequest
		refuse      error
		wantLabel   string
		wantSent    proto.Message
		wantMessage string
	}{
		{
			name:        "a queue the forge refuses says why",
			pullRequest: newQueueablePullRequest(),
			refuse:      connect.NewError(connect.CodeFailedPrecondition, errors.New("github will not change auto-merge on #740: Head sha didn't match expected")),
			wantLabel:   "merge when ready",
			wantSent: &forgev1.SetForgePullRequestAutoMergeRequest{
				Url:     pullRequestURL,
				HeadSha: "new",
				On:      true,
			},
			wantMessage: "Head sha didn't match expected",
		},
		{
			name:        "a pull request the forge would queue is queued at the head the panel holds",
			pullRequest: newQueueablePullRequest(),
			wantLabel:   "merge when ready",
			wantSent: &forgev1.SetForgePullRequestAutoMergeRequest{
				Url:     pullRequestURL,
				HeadSha: "new",
				On:      true,
			},
			wantMessage: "api: #740 merges when ready",
		},
		{
			name:        "a queued pull request stops waiting",
			pullRequest: newAutoMergedPullRequest(),
			wantLabel:   "cancel auto-merge",
			wantSent:    &forgev1.SetForgePullRequestAutoMergeRequest{Url: pullRequestURL, HeadSha: "new"},
			wantMessage: "api: #740 no longer merges on its own",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openGitMenu(t, append(buildGitAgent(), tt.pullRequest)...)
			if tt.refuse != nil {
				harness.refuse(forgev1connect.ForgeServiceSetForgePullRequestAutoMergeProcedure, tt.refuse)
			}
			harness.press("m")
			require.Contains(t, harness.readFrame(), tt.wantLabel)

			harness.press("u")

			harness.assertSentTo(forgev1connect.ForgeServiceSetForgePullRequestAutoMergeProcedure, tt.wantSent)
			assert.NotContains(t, harness.readFrame(), tt.wantLabel)
			assert.Contains(t, readOpenMessages(harness), tt.wantMessage)
		})
	}
}

// TestGitMenuPushes covers the push entry asking first with what the push would do, then sending
// the branch and saying what went up.
func TestGitMenuPushes(t *testing.T) {
	t.Parallel()

	plan := &agentv1.PlanPushRequest{AgentId: "a1"}
	push := &agentv1.PushWorktreeRequest{AgentId: "a1"}

	tests := []struct {
		name        string
		pullRequest *pullrequestv1.PullRequest
		planned     *agentv1.PlanPushResponse
		refusePlan  error
		refusePush  error
		press       []string
		wantButton  string
		wantWarning string
		wantUnsaid  string
		wantPushes  []proto.Message
		wantMessage string
	}{
		{
			name:        "a plan the server refuses opens no confirmation",
			refusePlan:  connect.NewError(connect.CodeFailedPrecondition, errors.New("the branch holds nothing the base does not")),
			wantMessage: "the branch holds nothing the base does not",
		},
		{
			name:        "a branch origin already holds warns and opens no confirmation",
			planned:     &agentv1.PlanPushResponse{Branch: "agent/a1", InSync: true},
			wantMessage: "api: commit already pushed",
		},
		{
			name: "a push origin rejects says so",
			planned: &agentv1.PlanPushResponse{
				Branch:  "agent/a1",
				Commits: 2,
			},
			refusePush:  connect.NewError(connect.CodeFailedPrecondition, errors.New("origin has commits this branch never took in")),
			press:       []string{"y"},
			wantButton:  "Push",
			wantPushes:  []proto.Message{push},
			wantMessage: "origin has commits this branch never took in",
		},
		{
			name: "declining sends nothing",
			planned: &agentv1.PlanPushResponse{
				Branch:  "agent/a1",
				Commits: 2,
			},
			press:      []string{"esc"},
			wantButton: "Push",
		},
		{
			name: "a rebased branch is force-pushed",
			planned: &agentv1.PlanPushResponse{
				Branch:  "agent/a1",
				Commits: 3,
				Forced:  true,
			},
			press:       []string{"y"},
			wantButton:  "Force push",
			wantWarning: "be overwritten",
			wantPushes:  []proto.Message{push},
			wantMessage: "api: force-pushed agent/a1",
		},
		{
			name:        "a branch queued to merge warns that what goes up may land",
			pullRequest: newAutoMergedPullRequest(),
			planned: &agentv1.PlanPushResponse{
				Branch:  "agent/a1",
				Commits: 2,
			},
			press:       []string{"y"},
			wantButton:  "Push",
			wantWarning: "Auto-merge is",
			wantPushes:  []proto.Message{push},
			wantMessage: "api: pushed agent/a1",
		},
		{
			name: "files nothing has committed are named as staying behind",
			planned: &agentv1.PlanPushResponse{
				Branch:           "agent/a1",
				Commits:          2,
				UncommittedFiles: 4,
			},
			press:       []string{"y"},
			wantButton:  "Push",
			wantWarning: "nothing has committed will stay behind",
			wantPushes:  []proto.Message{push},
			wantMessage: "api: pushed agent/a1",
		},
		{
			name: "a branch is pushed",
			planned: &agentv1.PlanPushResponse{
				Branch:  "agent/a1",
				Commits: 2,
			},
			press:       []string{"y"},
			wantButton:  "Push",
			wantWarning: "2 commits will go up",
			wantUnsaid:  "overwritten",
			wantPushes:  []proto.Message{push},
			wantMessage: "api: pushed agent/a1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rows := buildGitAgent()
			if tt.pullRequest != nil {
				rows = append(rows, tt.pullRequest)
			}
			harness := openGitMenu(t, rows...)
			if tt.refusePlan != nil {
				harness.refuse(agentv1connect.AgentServicePlanPushProcedure, tt.refusePlan)
			}
			if tt.planned != nil {
				harness.answer(agentv1connect.AgentServicePlanPushProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
					return connect.NewResponse(tt.planned), nil
				})
			}
			harness.answerPushed(tt.planned)
			if tt.refusePush != nil {
				harness.refuse(agentv1connect.AgentServicePushWorktreeProcedure, tt.refusePush)
			}

			harness.press("p")

			harness.assertSentTo(agentv1connect.AgentServicePlanPushProcedure, plan)
			if tt.wantButton != "" {
				require.Contains(t, harness.readFrame(), tt.wantButton)
			}
			if tt.wantWarning != "" {
				require.Contains(t, harness.readFrame(), tt.wantWarning)
			}
			if tt.wantUnsaid != "" {
				require.NotContains(t, harness.readFrame(), tt.wantUnsaid)
			}
			harness.press(tt.press...)

			harness.assertSentTo(agentv1connect.AgentServicePushWorktreeProcedure, tt.wantPushes...)
			assert.NotContains(t, harness.readFrame(), "Push the branch of")
			if tt.wantMessage != "" {
				assert.Contains(t, readOpenMessages(harness), tt.wantMessage)
			}
		})
	}
}

// TestGitMenuAborts covers the abort entry naming what is in progress, asking first, then undoing
// it and saying where the branch stands.
func TestGitMenuAborts(t *testing.T) {
	t.Parallel()

	abort := &agentv1.AbortSequenceRequest{AgentId: "a1"}

	tests := []struct {
		name        string
		sequence    taskv1.Sequence
		noun        string
		refuse      error
		press       []string
		wantAborts  []proto.Message
		wantMessage string
	}{
		{
			name:        "an abort the server refuses says why",
			refuse:      connect.NewError(connect.CodeFailedPrecondition, errors.New("another git command is running in this worktree")),
			press:       []string{"y"},
			wantAborts:  []proto.Message{abort},
			wantMessage: "another git command is running in this worktree",
		},
		{
			name:  "declining sends nothing",
			press: []string{"esc"},
		},
		{
			name:        "an abort says where the branch stands and where the copy is",
			press:       []string{"y"},
			wantAborts:  []proto.Message{abort},
			wantMessage: "api: aborted the rebase agent/a1 is back where it stood before. A copy of the worktree from before the abort is kept at refs/komrad/aborted/a1/1.",
		},
		{
			name:        "an operation a newer server added is offered and named neutrally",
			sequence:    taskv1.Sequence(99),
			noun:        "git operation",
			press:       []string{"y"},
			wantAborts:  []proto.Message{abort},
			wantMessage: "api: aborted the git operation agent/a1 is back where it stood before.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sequence, noun := taskv1.Sequence_SEQUENCE_REBASE, "rebase"
			if tt.sequence != taskv1.Sequence_SEQUENCE_UNSPECIFIED {
				sequence, noun = tt.sequence, tt.noun
			}
			rows := buildGitAgent()
			rows[2].(*taskv1.Task).Sequence = sequence

			harness := openGitMenu(t, rows...)
			harness.answer(agentv1connect.AgentServiceAbortSequenceProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
				return connect.NewResponse(&agentv1.AbortSequenceResponse{
					Sequence:  sequence,
					Branch:    "agent/a1",
					ParkedRef: "refs/komrad/aborted/a1/1",
				}), nil
			})
			if tt.refuse != nil {
				harness.refuse(agentv1connect.AgentServiceAbortSequenceProcedure, tt.refuse)
			}
			require.Contains(t, harness.readFrame(), "abort "+noun)

			harness.press("x")

			require.Contains(t, harness.readFrame(), "Abort the "+noun+" in the worktree of api?")
			harness.press(tt.press...)

			harness.assertSentTo(agentv1connect.AgentServiceAbortSequenceProcedure, tt.wantAborts...)
			assert.NotContains(t, harness.readFrame(), "Abort the "+noun+" in")
			if tt.wantMessage != "" {
				assert.Contains(t, readOpenMessages(harness), tt.wantMessage)
			}
		})
	}
}

// TestGitMenuTogglesDraft covers the entry naming which way it goes and flipping the draft.
func TestGitMenuTogglesDraft(t *testing.T) {
	t.Parallel()

	drafted := newOpenPullRequest()
	drafted.State = pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT
	drafted.Draft = true

	tests := []struct {
		name        string
		pullRequest *pullrequestv1.PullRequest
		refuse      error
		wantLabel   string
		wantSent    proto.Message
		wantMessage string
	}{
		{
			name:        "an open pull request is made a draft",
			pullRequest: newOpenPullRequest(),
			wantLabel:   "toggle draft on",
			wantSent:    &forgev1.SetForgePullRequestDraftRequest{Url: pullRequestURL, Draft: true},
			wantMessage: "api: #740 is a draft",
		},
		{
			name:        "a draft is marked ready for review",
			pullRequest: drafted,
			wantLabel:   "toggle draft off",
			wantSent:    &forgev1.SetForgePullRequestDraftRequest{Url: pullRequestURL},
			wantMessage: "api: #740 is ready for review",
		},
		{
			name:        "a change the forge refuses says why",
			pullRequest: newOpenPullRequest(),
			refuse:      connect.NewError(connect.CodeFailedPrecondition, errors.New("github will not change #740: Draft pull requests are not supported")),
			wantLabel:   "toggle draft on",
			wantSent:    &forgev1.SetForgePullRequestDraftRequest{Url: pullRequestURL, Draft: true},
			wantMessage: "Draft pull requests are not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openGitMenu(t, append(buildGitAgent(), tt.pullRequest)...)
			if tt.refuse != nil {
				harness.refuse(forgev1connect.ForgeServiceSetForgePullRequestDraftProcedure, tt.refuse)
			}
			require.Contains(t, harness.readFrame(), tt.wantLabel)

			harness.press("d")

			harness.assertSentTo(forgev1connect.ForgeServiceSetForgePullRequestDraftProcedure, tt.wantSent)
			assert.NotContains(t, harness.readFrame(), gitMenuEntry)
			assert.Contains(t, readOpenMessages(harness), tt.wantMessage)
		})
	}
}

// TestGitMenuRequestsRereview covers the entry asking the reviewers who requested changes to look
// again, and doing nothing on a pull request nobody asked changes of.
func TestGitMenuRequestsRereview(t *testing.T) {
	t.Parallel()

	changesRequested := newOpenPullRequest()
	changesRequested.State = pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED
	rereviewRequested := newOpenPullRequest()
	rereviewRequested.State = pullrequestv1.PullRequestState_PULL_REQUEST_STATE_REREVIEW_REQUESTED

	tests := []struct {
		name        string
		pullRequest *pullrequestv1.PullRequest
		refuse      error
		wantSent    []proto.Message
		wantMessage string
	}{
		{
			name:        "a pull request with changes requested is sent back to its reviewers",
			pullRequest: changesRequested,
			wantSent:    []proto.Message{&forgev1.RequestForgePullRequestRereviewRequest{Url: pullRequestURL}},
			wantMessage: "api: #740 is waiting on another review",
		},
		{
			name:        "a pull request already waiting on another review is asked again",
			pullRequest: rereviewRequested,
			wantSent:    []proto.Message{&forgev1.RequestForgePullRequestRereviewRequest{Url: pullRequestURL}},
			wantMessage: "api: #740 is waiting on another review",
		},
		{
			name:        "a request the forge refuses says why",
			pullRequest: changesRequested,
			refuse:      connect.NewError(connect.CodeFailedPrecondition, errors.New("nobody has asked for changes on #740")),
			wantSent:    []proto.Message{&forgev1.RequestForgePullRequestRereviewRequest{Url: pullRequestURL}},
			wantMessage: "nobody has asked for changes on #740",
		},
		{
			name:        "a pull request nobody asked changes of sends nothing",
			pullRequest: newOpenPullRequest(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openGitMenu(t, append(buildGitAgent(), tt.pullRequest)...)
			if tt.refuse != nil {
				harness.refuse(forgev1connect.ForgeServiceRequestForgePullRequestRereviewProcedure, tt.refuse)
			}
			require.Contains(t, harness.readFrame(), "re-request review")

			harness.press("v")

			harness.assertSentTo(forgev1connect.ForgeServiceRequestForgePullRequestRereviewProcedure, tt.wantSent...)
			if tt.wantMessage != "" {
				assert.NotContains(t, harness.readFrame(), gitMenuEntry)
				assert.Contains(t, readOpenMessages(harness), tt.wantMessage)
			}
		})
	}
}

// TestGitMenuOpensThePullRequest covers the entry handing the browser the current pull request.
func TestGitMenuOpensThePullRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		refuse    error
		wantFrame string
	}{
		{
			name:      "a browser that refuses is reported",
			refuse:    errors.New("no application knows how to open it"),
			wantFrame: "Could not open",
		},
		{
			name: "the address is handed over",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opener := &recordingOpener{err: tt.refuse}
			harness := newPanelHarness(t, panelHarnessParams{browser: opener})
			answerWorktreeReads(harness)
			harness.watch(buildSnapshot(append(buildGitAgent(), newOpenPullRequest())...))
			harness.press("space", "g", "o")

			assert.Equal(t, []string{pullRequestURL}, opener.readTargets())
			frame := harness.readFrame()
			assert.NotContains(t, frame, gitMenuEntry)
			if tt.wantFrame == "" {
				assert.NotContains(t, frame, "Could not")

				return
			}
			assert.Contains(t, frame, tt.wantFrame)
		})
	}
}

// TestGitMenuCopiesThePullRequestURL covers the entry putting the current pull request's address
// on the clipboard.
func TestGitMenuCopiesThePullRequestURL(t *testing.T) {
	t.Parallel()

	harness := openGitMenu(t, append(buildGitAgent(), newOpenPullRequest())...)

	harness.press("c")

	assert.Equal(t, []string{pullRequestURL}, harness.panel.ReadCopied())
	frame := harness.readFrame()
	assert.NotContains(t, frame, gitMenuEntry)
	assert.NotContains(t, frame, "Could not")
}

const (
	gitMenuEntry   = "rebase with base"
	pullRequestURL = "https://github.com/harrisoncramer/komrad/pull/740"
)

type recordingOpener struct {
	mu      sync.Mutex
	targets []string
	err     error
}

func (r *recordingOpener) OpenURL(_ context.Context, target string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.targets = append(r.targets, target)

	return r.err
}

func (r *recordingOpener) readTargets() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.targets
}

func openGitMenu(t *testing.T, rows ...proto.Message) *panelHarness {
	t.Helper()

	harness := openAgents(t, rows...)
	harness.press("space", "g")
	require.Contains(t, harness.readFrame(), gitMenuEntry)

	return harness
}

func buildGitAgent() []proto.Message {
	agent, task := newBranchAgent("a1", "api", "agent/a1")
	agent.Cwd = "/trees/a1"
	task.Cwd = xptr.Ptr("/trees/a1")
	task.HeadSha = xptr.Ptr("new")

	return []proto.Message{
		&workflowv1.Workflow{
			Id:       "w1",
			Name:     "backend",
			Worktree: &workflowv1.WorktreeConfig{Enabled: true},
		},
		agent,
		task,
	}
}

func buildUncutAgent() []proto.Message {
	rows := buildGitAgent()
	rows[0] = newLiveWorkflow("w1", "backend")

	return rows
}

func newOpenPullRequest() *pullrequestv1.PullRequest {
	return &pullrequestv1.PullRequest{
		Id:                "pr-740",
		RepositoryUrl:     repositoryURL,
		HeadRepositoryUrl: repositoryURL,
		HeadBranch:        "agent/a1",
		Number:            740,
		Url:               pullRequestURL,
		State:             pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED,
		OpenedAt:          "2026-09-19T12:00:00Z",
	}
}

func newMergedPullRequest() *pullrequestv1.PullRequest {
	merged := newOpenPullRequest()
	merged.State = pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED
	merged.MergedAt = xptr.Ptr("2026-09-20T12:00:00Z")
	merged.HeadSha = "old"

	return merged
}

func newCurrentMergedPullRequest() *pullrequestv1.PullRequest {
	merged := newMergedPullRequest()
	merged.HeadSha = "new"

	return merged
}

func buildRebasingGitAgent() []proto.Message {
	rows := buildGitAgent()
	rows[2].(*taskv1.Task).Sequence = taskv1.Sequence_SEQUENCE_REBASE

	return rows
}

// answerPushed has the push answer with what the plan said it would send.
func (h *panelHarness) answerPushed(planned *agentv1.PlanPushResponse) {
	if planned == nil {
		return
	}

	h.answer(agentv1connect.AgentServicePushWorktreeProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&agentv1.PushWorktreeResponse{
			Branch:  planned.GetBranch(),
			Commits: planned.GetCommits(),
			Forced:  planned.GetForced(),
			Sha:     "abcdef1234567",
		}), nil
	})
}

func newQueueablePullRequest() *pullrequestv1.PullRequest {
	queueable := newOpenPullRequest()
	queueable.State = pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING
	queueable.HeadSha = "new"
	queueable.MergeReadiness = &pullrequestv1.MergeReadiness{AutoMergeable: true}

	return queueable
}

func newAutoMergedPullRequest() *pullrequestv1.PullRequest {
	queued := newQueueablePullRequest()
	queued.MergeReadiness = &pullrequestv1.MergeReadiness{AutoMerge: true}

	return queued
}

func newQueueableDraft() *pullrequestv1.PullRequest {
	draft := newQueueablePullRequest()
	draft.State = pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT
	draft.Draft = true

	return draft
}

func onGitLab(pullRequest *pullrequestv1.PullRequest) *pullrequestv1.PullRequest {
	pullRequest.Forge = "gitlab"

	return pullRequest
}
