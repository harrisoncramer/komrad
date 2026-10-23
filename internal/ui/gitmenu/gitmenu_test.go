package gitmenu_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"komrad/internal/ui/paneltest"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	forgev1 "komrad/proto/gen/forge/v1"
	"komrad/proto/gen/forge/v1/forgev1connect"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	taskv1 "komrad/proto/gen/task/v1"
	"komrad/proto/gen/task/v1/taskv1connect"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestGitMenu covers the menu of git actions rendered over the agent the cursor is on.
func TestGitMenu(t *testing.T) {
	t.Parallel()

	t.Run("the menu opens it to its own right and names both in the trail", func(t *testing.T) {
		t.Parallel()

		harness := openGitMenu(t, buildGitAgent()...)

		frame := harness.Frame()
		assert.Equal(t, "agents › task-a1 › git", paneltest.Trail(frame))
		assert.Contains(t, frame, "  › git ›", "the menu it came from stays rendered with its cursor on git")
	})

	t.Run("esc goes back to the menu it came from, its cursor still on git", func(t *testing.T) {
		t.Parallel()

		harness := openGitMenu(t, buildGitAgent()...)

		harness.Press("esc")

		frame := harness.Frame()
		assert.NotContains(t, frame, gitMenuEntry)
		assert.Contains(t, frame, "› git ›")
		assert.Equal(t, "agents › task-a1", paneltest.Trail(frame))

		harness.Press("esc")

		frame = harness.Frame()
		assert.NotContains(t, frame, "git ›")
		assert.NotContains(t, frame, "Could not")
	})

	t.Run("back does what esc does", func(t *testing.T) {
		t.Parallel()

		harness := openGitMenu(t, buildGitAgent()...)

		harness.Press("k", "enter")

		frame := harness.Frame()
		assert.NotContains(t, frame, gitMenuEntry)
		assert.Contains(t, frame, "› git ›")
	})

	t.Run("a narrow panel renders only the menu on top", func(t *testing.T) {
		t.Parallel()

		harness := paneltest.Start(t, paneltest.Params{Width: 36, Height: 20})
		harness.AnswerWorktreeReads()
		harness.Watch(paneltest.Snapshot(buildGitAgent()...))
		harness.MoveToRow("task-a1")
		harness.Press("space", "g")

		frame := harness.Frame()
		assert.Contains(t, frame, gitMenuEntry)
		assert.NotContains(t, frame, "documents")
	})

	t.Run("an entry answers its own letter", func(t *testing.T) {
		t.Parallel()

		harness := openGitMenu(t, buildGitAgent()...)

		harness.Press("r")

		assert.NotContains(t, harness.Frame(), gitMenuEntry)
		harness.AssertSentTo(agentv1connect.AgentServicePlanRebaseProcedure, &agentv1.PlanRebaseRequest{Task: &agentv1.PlanRebaseRequest_TaskId{TaskId: "task-a1"}})
	})

	t.Run("abort sits with the worktree entries, ahead of push", func(t *testing.T) {
		t.Parallel()

		frame := openGitMenu(t, buildGitAgent()...).Frame()

		require.Contains(t, frame, "abort")
		require.Contains(t, frame, "push")
		assert.Less(t, strings.Index(frame, "abort"), strings.Index(frame, "push"))
	})

	t.Run("the forge menu holds the pull request's entries and the ways to merge it", func(t *testing.T) {
		t.Parallel()

		harness := openGitMenu(t, append(buildGitAgent(), newQueueablePullRequest())...)
		require.NotContains(t, harness.Frame(), "open pr")

		harness.Press("f")

		frame := harness.Frame()
		assert.Equal(t, "agents › task-a1 › git › forge", paneltest.Trail(frame))
		for _, entry := range []string{"open pr", "copy pr url", "toggle draft on", "re-request review", "merge and archive", "merge when ready", "merge then rebase"} {
			assert.Contains(t, frame, entry)
		}
	})

	t.Run("a merge request's entries are named the way GitLab does", func(t *testing.T) {
		t.Parallel()

		harness := openGitMenu(t, append(buildGitAgent(), onGitLab(newOpenPullRequest()))...)

		harness.Press("f")

		frame := harness.Frame()
		assert.Contains(t, frame, "open mr")
		assert.Contains(t, frame, "copy mr url")
		assert.NotContains(t, frame, "open pr")
	})

	t.Run("the menu takes no filter, so the letters stay the entries'", func(t *testing.T) {
		t.Parallel()

		harness := openGitMenu(t, buildGitAgent()...)
		require.NotContains(t, harness.Frame(), "filter")

		harness.Press("/")

		assert.Contains(t, harness.Frame(), "rebase with base")
	})

	t.Run("an entry picked after its task has left the list says so", func(t *testing.T) {
		t.Parallel()

		harness := openGitMenu(t, buildGitAgent()...)
		harness.Watch(paneltest.Snapshot(buildGitAgent()[0]))

		harness.Press("r")

		assert.Contains(t, harness.ReadMessages(), "the task the menu was opened on has left the list")
		harness.AssertSentTo(agentv1connect.AgentServicePlanRebaseProcedure)
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
			name:        "the rebase of a task with no worktree of its own",
			rows:        buildUncutAgent(),
			press:       []string{"r"},
			wantRefusal: "this task has no worktree of its own for komrad to act on",
		},
		{
			name:        "the push of a task with no worktree of its own",
			rows:        buildUncutAgent(),
			press:       []string{"p"},
			wantRefusal: "this task has no worktree of its own for komrad to act on",
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
			press:       []string{"f", "o"},
			wantRefusal: "komrad has not read this task's repository yet",
		},
		{
			name:        "the pull request of a branch with none",
			rows:        append(buildUncutAgent(), &pullrequestv1.Repository{Url: repositoryURL}),
			press:       []string{"f", "o"},
			wantRefusal: "nobody has opened a pull request from this branch",
		},
		{
			name:        "the pull request of a branch that has moved past its last one",
			rows:        append(buildUncutAgent(), newMergedPullRequest()),
			press:       []string{"f", "o"},
			wantRefusal: "the branch moved on after its last pull request finished",
		},
		{
			name:        "the copy of a pull request address on a branch with none",
			rows:        buildUncutAgent(),
			press:       []string{"f", "c"},
			wantRefusal: "komrad has not read this task's repository yet",
		},
		{
			name:        "the merge of a branch with no pull request",
			rows:        buildUncutAgent(),
			press:       []string{"f", "m"},
			wantRefusal: "komrad has not read this task's repository yet",
		},
		{
			name:        "the merge of a pull request that has already merged",
			rows:        append(buildGitAgent(), newCurrentMergedPullRequest()),
			press:       []string{"f", "m"},
			wantRefusal: "its pull request has already merged",
		},
		{
			name:        "the merge when ready of a draft",
			rows:        append(buildGitAgent(), newQueueableDraft()),
			press:       []string{"f", "u"},
			wantRefusal: "its pull request is a draft, so mark it ready first",
		},
		{
			name:        "the merge when ready of a pull request the forge will not queue",
			rows:        append(buildGitAgent(), newOpenPullRequest()),
			press:       []string{"f", "u"},
			wantRefusal: "the forge refused to queue it",
		},
		{
			name:        "the merge then rebase of a worktree part-way through a rebase",
			rows:        append(buildRebasingGitAgent(), newOpenPullRequest()),
			press:       []string{"f", "r"},
			wantRefusal: "part-way through a rebase, merge or pick",
		},
		{
			name:        "the merge then rebase of a branch with no pull request",
			rows:        buildGitAgent(),
			press:       []string{"f", "r"},
			wantRefusal: "komrad has not read this task's repository yet",
		},
		{
			name:        "the merge then rebase of a pull request a forge rule blocks",
			rows:        append(buildGitAgent(), withMergeability(newOpenPullRequest(), pullrequestv1.Mergeability_MERGEABILITY_BLOCKED)),
			press:       []string{"f", "r"},
			wantRefusal: "its pull request is blocked by a rule on the forge, so merge it when ready instead",
		},
		{
			name:        "the merge then rebase of a draft the forge blocks",
			rows:        append(buildGitAgent(), withMergeability(newQueueableDraft(), pullrequestv1.Mergeability_MERGEABILITY_BLOCKED)),
			press:       []string{"f", "r"},
			wantRefusal: "its pull request is a draft, so mark it ready first",
		},
		{
			name:        "the merge then rebase of a pull request behind its base branch",
			rows:        append(buildGitAgent(), withMergeability(newOpenPullRequest(), pullrequestv1.Mergeability_MERGEABILITY_BEHIND)),
			press:       []string{"f", "r"},
			wantRefusal: "its pull request is behind the base branch, so rebase it first",
		},
		{
			name:        "the merge then rebase of a pull request that conflicts with its base branch",
			rows:        append(buildGitAgent(), withMergeability(newOpenPullRequest(), pullrequestv1.Mergeability_MERGEABILITY_CONFLICTING)),
			press:       []string{"f", "r"},
			wantRefusal: "its pull request conflicts with the base branch, so rebase it first",
		},
		{
			name:        "the draft toggle of a pull request that has already merged",
			rows:        append(buildGitAgent(), newCurrentMergedPullRequest()),
			press:       []string{"f", "d"},
			wantRefusal: "its pull request has already merged",
		},
		{
			name:        "the merge of a merge request that has already merged",
			rows:        append(buildGitAgent(), onGitLab(newCurrentMergedPullRequest())),
			press:       []string{"f", "m"},
			wantRefusal: "its merge request has already merged",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openGitMenu(t, tt.rows...)

			harness.Press(tt.press...)

			assert.NotContains(t, harness.Frame(), gitMenuEntry)
			assert.Contains(t, harness.ReadMessages(), tt.wantRefusal)
		})
	}
}

// TestGitMenuMerges covers the forge menu merging the current pull request and archiving the
// agent after.
func TestGitMenuMerges(t *testing.T) {
	t.Parallel()

	merge := &forgev1.MergeForgePullRequestRequest{
		Url:     pullRequestURL,
		Method:  forgev1.MergeMethod_MERGE_METHOD_SQUASH,
		HeadSha: "new",
	}

	tests := []struct {
		name        string
		forge       forgev1.Kind
		press       []string
		refuse      error
		wantMerges  []proto.Message
		wantArchive []proto.Message
		wantMessage string
	}{
		{
			name:        "merge leaves the task where it is",
			press:       []string{"m", "enter"},
			wantMerges:  []proto.Message{merge},
			wantMessage: "task-a1: merged #740",
		},
		{
			name:        "merge names a merge request the way GitLab does",
			forge:       forgev1.Kind_KIND_GITLAB,
			press:       []string{"m", "enter"},
			wantMerges:  []proto.Message{merge},
			wantMessage: "task-a1: merged !740",
		},
		{
			name:        "merge and archive archives the task once the forge has merged",
			press:       []string{"a", "enter"},
			wantMerges:  []proto.Message{merge},
			wantArchive: []proto.Message{&taskv1.ArchiveTaskRequest{Task: &taskv1.ArchiveTaskRequest_TaskId{TaskId: "task-a1"}}},
			wantMessage: "task-a1: merged #740",
		},
		{
			name:  "back sends nothing",
			press: []string{"esc"},
		},
		{
			name:        "a merge the forge refuses archives nothing",
			press:       []string{"a", "enter"},
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
			pullRequest.HeadSha = "new"
			harness := openGitMenu(t, append(buildGitAgent(), pullRequest)...)
			answerMergePlan(harness, newMergePlan())
			if tt.refuse != nil {
				harness.Refuse(forgev1connect.ForgeServiceMergeForgePullRequestProcedure, tt.refuse)
			}

			harness.Press("f")
			require.Equal(t, "agents › task-a1 › git › forge", paneltest.Trail(harness.Frame()))
			harness.Press(tt.press...)

			harness.AssertSentTo(forgev1connect.ForgeServiceMergeForgePullRequestProcedure, tt.wantMerges...)
			harness.AssertSentTo(taskv1connect.TaskServiceArchiveTaskProcedure, tt.wantArchive...)
			assert.NotContains(t, harness.Frame(), "merge and archive")
			if tt.wantMessage != "" {
				assert.Contains(t, harness.ReadMessages(), tt.wantMessage)
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
			wantRebases:  []proto.Message{&agentv1.RebaseWorktreeRequest{Task: &agentv1.RebaseWorktreeRequest_TaskId{TaskId: "task-a1"}}},
			wantMessages: []string{"task-a1: merged #740", "task-a1: moved onto main"},
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
			answerMergePlan(harness, newMergePlan())
			if tt.refuse != nil {
				harness.Refuse(forgev1connect.ForgeServiceMergeForgePullRequestProcedure, tt.refuse)
			}
			harness.Answer(agentv1connect.AgentServiceRebaseWorktreeProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
				return connect.NewResponse(&agentv1.RebaseWorktreeResponse{
					BaseRef: "main",
					BaseSha: "abcdef1234567",
					Moved:   true,
				}), nil
			})

			harness.Press("f", "r", "enter")

			harness.AssertSentTo(forgev1connect.ForgeServiceMergeForgePullRequestProcedure, &forgev1.MergeForgePullRequestRequest{
				Url:     pullRequestURL,
				Method:  forgev1.MergeMethod_MERGE_METHOD_SQUASH,
				HeadSha: "planned",
			})
			harness.AssertSentTo(agentv1connect.AgentServiceRebaseWorktreeProcedure, tt.wantRebases...)
			harness.AssertSentTo(agentv1connect.AgentServicePlanRebaseProcedure)
			assert.NotContains(t, harness.Frame(), "merge then rebase")
			messages := harness.ReadMessages()
			for _, want := range tt.wantMessages {
				assert.Contains(t, messages, want)
			}
		})
	}
}

// TestGitMenuMergesWhenReady covers the forge menu entry naming which way it goes, asking the
// forge to merge on its own the way the form says, and stopping it again at once.
func TestGitMenuMergesWhenReady(t *testing.T) {
	t.Parallel()

	queue := &forgev1.SetForgePullRequestAutoMergeRequest{
		Url:     pullRequestURL,
		HeadSha: "new",
		On:      true,
		Method:  forgev1.MergeMethod_MERGE_METHOD_SQUASH,
	}

	tests := []struct {
		name        string
		pullRequest *pullrequestv1.PullRequest
		press       []string
		refuse      error
		wantLabel   string
		wantPlans   []proto.Message
		wantSent    proto.Message
		wantMessage string
	}{
		{
			name:        "a queue the forge refuses says why",
			pullRequest: newQueueablePullRequest(),
			press:       []string{"u", "enter"},
			refuse:      connect.NewError(connect.CodeFailedPrecondition, errors.New("github will not change auto-merge on #740: Head sha didn't match expected")),
			wantLabel:   "merge when ready",
			wantPlans:   []proto.Message{&forgev1.PlanForgePullRequestMergeRequest{Url: pullRequestURL}},
			wantSent:    queue,
			wantMessage: "Head sha didn't match expected",
		},
		{
			name:        "a pull request the forge would queue is queued at the head the panel holds",
			pullRequest: newQueueablePullRequest(),
			press:       []string{"u", "enter"},
			wantLabel:   "merge when ready",
			wantPlans:   []proto.Message{&forgev1.PlanForgePullRequestMergeRequest{Url: pullRequestURL}},
			wantSent:    queue,
			wantMessage: "task-a1: #740 merges when ready",
		},
		{
			name:        "a queued pull request stops waiting without asking how it would merge",
			pullRequest: newAutoMergedPullRequest(),
			press:       []string{"u"},
			wantLabel:   "cancel auto-merge",
			wantSent:    &forgev1.SetForgePullRequestAutoMergeRequest{Url: pullRequestURL, HeadSha: "new"},
			wantMessage: "task-a1: #740 no longer merges on its own",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openGitMenu(t, append(buildGitAgent(), tt.pullRequest)...)
			answerMergePlan(harness, newMergePlan())
			if tt.refuse != nil {
				harness.Refuse(forgev1connect.ForgeServiceSetForgePullRequestAutoMergeProcedure, tt.refuse)
			}
			harness.Press("f")
			require.Contains(t, harness.Frame(), tt.wantLabel)

			harness.Press(tt.press...)

			harness.AssertSentTo(forgev1connect.ForgeServicePlanForgePullRequestMergeProcedure, tt.wantPlans...)
			harness.AssertSentTo(forgev1connect.ForgeServiceSetForgePullRequestAutoMergeProcedure, tt.wantSent)
			frame := harness.Frame()
			assert.NotContains(t, frame, tt.wantLabel)
			assert.NotContains(t, frame, "Merge #740 when ready")
			assert.Contains(t, harness.ReadMessages(), tt.wantMessage)
		})
	}
}

// TestGitMenuMergeForm covers the form a merge goes through, opening on the forge's default
// method with its message and sending only what the person changed.
func TestGitMenuMergeForm(t *testing.T) {
	t.Parallel()

	t.Run("the form opens on the forge's default method with its message", func(t *testing.T) {
		t.Parallel()

		harness := openGitMenu(t, append(buildGitAgent(), newOpenPullRequest())...)
		answerMergePlan(harness, newMergePlan())

		harness.Press("f", "m")

		frame := harness.Frame()
		assert.Contains(t, frame, "Merge #740")
		assert.Contains(t, frame, "merge commit  not allowed")
		assert.Contains(t, frame, "Add the merge form (#740)")
		assert.Contains(t, frame, "Pick a method before merging.")
		harness.AssertSentTo(forgev1connect.ForgeServicePlanForgePullRequestMergeProcedure, &forgev1.PlanForgePullRequestMergeRequest{Url: pullRequestURL})
		harness.AssertSentTo(forgev1connect.ForgeServiceMergeForgePullRequestProcedure)
	})

	tests := []struct {
		name        string
		press       []string
		wantMerges  []proto.Message
		wantRefusal string
	}{
		{
			name:  "a method that writes no commit sends no message",
			press: []string{"down", "enter"},
			wantMerges: []proto.Message{&forgev1.MergeForgePullRequestRequest{
				Url:     pullRequestURL,
				Method:  forgev1.MergeMethod_MERGE_METHOD_REBASE,
				HeadSha: "planned",
			}},
		},
		{
			name:  "an edited headline goes with the merge without the body",
			press: slices.Concat([]string{"tab"}, strings.Split(" now", ""), []string{"enter"}),
			wantMerges: []proto.Message{&forgev1.MergeForgePullRequestRequest{
				Url:      pullRequestURL,
				Method:   forgev1.MergeMethod_MERGE_METHOD_SQUASH,
				Headline: xptr.Ptr("Add the merge form (#740) now"),
				HeadSha:  "planned",
			}},
		},
		{
			name:  "a method the base branch refuses is still sent for the forge to answer",
			press: []string{"up", "enter"},
			wantMerges: []proto.Message{&forgev1.MergeForgePullRequestRequest{
				Url:     pullRequestURL,
				Method:  forgev1.MergeMethod_MERGE_METHOD_MERGE,
				HeadSha: "planned",
			}},
		},
		{
			name:  "an edited body goes with the merge without the headline",
			press: slices.Concat([]string{"tab", "tab"}, strings.Split(" Twice.", ""), []string{"enter"}),
			wantMerges: []proto.Message{&forgev1.MergeForgePullRequestRequest{
				Url:     pullRequestURL,
				Method:  forgev1.MergeMethod_MERGE_METHOD_SQUASH,
				Body:    xptr.Ptr("Pick a method before merging. Twice."),
				HeadSha: "planned",
			}},
		},
		{
			name:        "a cleared headline is not sent",
			press:       []string{"tab", "ctrl+u", "enter"},
			wantRefusal: "a commit needs a headline",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openGitMenu(t, append(buildGitAgent(), newOpenPullRequest())...)
			answerMergePlan(harness, newMergePlan())
			harness.Press("f", "m")

			harness.Press(tt.press...)

			harness.AssertSentTo(forgev1connect.ForgeServiceMergeForgePullRequestProcedure, tt.wantMerges...)
			if tt.wantRefusal != "" {
				assert.Contains(t, harness.Frame(), tt.wantRefusal)
			}
		})
	}

	t.Run("a forge that cannot say how it would merge says why", func(t *testing.T) {
		t.Parallel()

		harness := openGitMenu(t, append(buildGitAgent(), newOpenPullRequest())...)
		harness.Refuse(forgev1connect.ForgeServicePlanForgePullRequestMergeProcedure, connect.NewError(connect.CodeUnavailable, errors.New("github is down")))

		harness.Press("f", "m")

		assert.NotContains(t, harness.Frame(), "Merge #740")
		assert.Contains(t, harness.ReadMessages(), "github is down")
		harness.AssertSentTo(forgev1connect.ForgeServiceMergeForgePullRequestProcedure)
	})
}

// TestGitMenuPushes covers the push entry asking first with what the push would do, then sending
// the branch and saying what went up.
func TestGitMenuPushes(t *testing.T) {
	t.Parallel()

	plan := &agentv1.PlanPushRequest{Task: &agentv1.PlanPushRequest_TaskId{TaskId: "task-a1"}}
	push := &agentv1.PushWorktreeRequest{Task: &agentv1.PushWorktreeRequest_TaskId{TaskId: "task-a1"}}

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
			wantMessage: "task-a1: commit already pushed",
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
			wantWarning: "so komrad overwrites it",
			wantPushes:  []proto.Message{push},
			wantMessage: "task-a1: force-pushed agent/a1",
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
			wantWarning: "so the forge merges the push",
			wantPushes:  []proto.Message{push},
			wantMessage: "task-a1: pushed agent/a1",
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
			wantWarning: "of uncommitted changes behind",
			wantPushes:  []proto.Message{push},
			wantMessage: "task-a1: pushed agent/a1",
		},
		{
			name: "a branch is pushed",
			planned: &agentv1.PlanPushResponse{
				Branch:  "agent/a1",
				Commits: 2,
			},
			press:       []string{"y"},
			wantButton:  "Push",
			wantWarning: "Komrad pushes 2 commits",
			wantUnsaid:  "overwritten",
			wantPushes:  []proto.Message{push},
			wantMessage: "task-a1: pushed agent/a1",
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
				harness.Refuse(agentv1connect.AgentServicePlanPushProcedure, tt.refusePlan)
			}
			if tt.planned != nil {
				harness.Answer(agentv1connect.AgentServicePlanPushProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
					return connect.NewResponse(tt.planned), nil
				})
			}
			answerPushed(harness, tt.planned)
			if tt.refusePush != nil {
				harness.Refuse(agentv1connect.AgentServicePushWorktreeProcedure, tt.refusePush)
			}

			harness.Press("p")

			harness.AssertSentTo(agentv1connect.AgentServicePlanPushProcedure, plan)
			if tt.wantButton != "" {
				require.Contains(t, harness.Frame(), tt.wantButton)
			}
			if tt.wantWarning != "" {
				require.Contains(t, harness.Frame(), tt.wantWarning)
			}
			if tt.wantUnsaid != "" {
				require.NotContains(t, harness.Frame(), tt.wantUnsaid)
			}
			harness.Press(tt.press...)

			harness.AssertSentTo(agentv1connect.AgentServicePushWorktreeProcedure, tt.wantPushes...)
			assert.NotContains(t, harness.Frame(), "Push the branch of")
			if tt.wantMessage != "" {
				assert.Contains(t, harness.ReadMessages(), tt.wantMessage)
			}
		})
	}
}

// TestGitMenuAborts covers the abort entry naming what is in progress, asking first, then undoing
// it and saying where the branch stands.
func TestGitMenuAborts(t *testing.T) {
	t.Parallel()

	abort := &agentv1.AbortSequenceRequest{Task: &agentv1.AbortSequenceRequest_TaskId{TaskId: "task-a1"}}

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
			wantMessage: "task-a1: aborted the rebase agent/a1 is back where it was before. Komrad saved a copy of the worktree from before the abort at refs/komrad/aborted/a1/1.",
		},
		{
			name:        "an operation a newer server added is offered and named neutrally",
			sequence:    taskv1.Sequence(99),
			noun:        "git operation",
			press:       []string{"y"},
			wantAborts:  []proto.Message{abort},
			wantMessage: "task-a1: aborted the git operation agent/a1 is back where it was before.",
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
			harness.Answer(agentv1connect.AgentServiceAbortSequenceProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
				return connect.NewResponse(&agentv1.AbortSequenceResponse{
					Sequence:  sequence,
					Branch:    "agent/a1",
					ParkedRef: "refs/komrad/aborted/a1/1",
				}), nil
			})
			if tt.refuse != nil {
				harness.Refuse(agentv1connect.AgentServiceAbortSequenceProcedure, tt.refuse)
			}
			require.Contains(t, harness.Frame(), "abort "+noun)

			harness.Press("x")

			require.Contains(t, harness.Frame(), "Abort the "+noun+" in the worktree of task-a1?")
			harness.Press(tt.press...)

			harness.AssertSentTo(agentv1connect.AgentServiceAbortSequenceProcedure, tt.wantAborts...)
			assert.NotContains(t, harness.Frame(), "Abort the "+noun+" in")
			if tt.wantMessage != "" {
				assert.Contains(t, harness.ReadMessages(), tt.wantMessage)
			}
		})
	}
}

// TestGitMenuPicksAForkPoint covers an orphaned task naming the commit its own commits begin on.
func TestGitMenuPicksAForkPoint(t *testing.T) {
	t.Parallel()

	parent := paneltest.Agent("a0", "web", "w1")
	rows := append(buildGitAgent(), parent, paneltest.TaskOf(parent))
	rows[2].(*taskv1.Task).Parent = &taskv1.Parent{TaskId: "task-a0", Blocked: taskv1.CascadeBlock_CASCADE_BLOCK_ORPHANED}

	harness := paneltest.OpenAgents(t, rows...)
	harness.MoveToRow("task-a1")

	harness.Press("space", "g", "s")

	require.Contains(t, harness.Frame(), `Fork point of "task-a1" on task-a0`)

	harness.Type("abc1234")
	harness.Press("enter")

	harness.AssertSentTo(agentv1connect.AgentServicePickForkPointProcedure, &agentv1.PickForkPointRequest{
		Task:   &agentv1.PickForkPointRequest_TaskId{TaskId: "task-a1"},
		Commit: "abc1234",
	})
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
			wantMessage: "task-a1: #740 is a draft",
		},
		{
			name:        "a draft is marked ready for review",
			pullRequest: drafted,
			wantLabel:   "toggle draft off",
			wantSent:    &forgev1.SetForgePullRequestDraftRequest{Url: pullRequestURL},
			wantMessage: "task-a1: #740 is ready for review",
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
				harness.Refuse(forgev1connect.ForgeServiceSetForgePullRequestDraftProcedure, tt.refuse)
			}
			harness.Press("f")
			require.Contains(t, harness.Frame(), tt.wantLabel)

			harness.Press("d")

			harness.AssertSentTo(forgev1connect.ForgeServiceSetForgePullRequestDraftProcedure, tt.wantSent)
			assert.NotContains(t, harness.Frame(), gitMenuEntry)
			assert.Contains(t, harness.ReadMessages(), tt.wantMessage)
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
			wantMessage: "task-a1: #740 is waiting on another review",
		},
		{
			name:        "a pull request already waiting on another review is asked again",
			pullRequest: rereviewRequested,
			wantSent:    []proto.Message{&forgev1.RequestForgePullRequestRereviewRequest{Url: pullRequestURL}},
			wantMessage: "task-a1: #740 is waiting on another review",
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
				harness.Refuse(forgev1connect.ForgeServiceRequestForgePullRequestRereviewProcedure, tt.refuse)
			}
			harness.Press("f")
			require.Contains(t, harness.Frame(), "re-request review")

			harness.Press("v")

			harness.AssertSentTo(forgev1connect.ForgeServiceRequestForgePullRequestRereviewProcedure, tt.wantSent...)
			if tt.wantMessage != "" {
				assert.NotContains(t, harness.Frame(), gitMenuEntry)
				assert.Contains(t, harness.ReadMessages(), tt.wantMessage)
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
			harness := paneltest.Start(t, paneltest.Params{Browser: opener})
			harness.AnswerWorktreeReads()
			harness.Watch(paneltest.Snapshot(append(buildGitAgent(), newOpenPullRequest())...))
			harness.MoveToRow("task-a1")
			harness.Press("space", "g", "f", "o")

			assert.Equal(t, []string{pullRequestURL}, opener.readTargets())
			frame := harness.Frame()
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

	harness.Press("f", "c")

	assert.Equal(t, []string{pullRequestURL}, harness.Copied())
	frame := harness.Frame()
	assert.NotContains(t, frame, gitMenuEntry)
	assert.NotContains(t, frame, "Could not")
}

const (
	gitMenuEntry   = "rebase with base"
	repositoryURL  = "https://github.com/harrisoncramer/komrad"
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

func openGitMenu(t *testing.T, rows ...proto.Message) *paneltest.Panel {
	t.Helper()

	harness := paneltest.OpenAgents(t, rows...)
	harness.MoveToRow("task-a1")
	harness.Press("space", "g")
	require.Contains(t, harness.Frame(), gitMenuEntry)

	return harness
}

func buildGitAgent() []proto.Message {
	agent := paneltest.Agent("a1", "api", "w1")
	task := paneltest.TaskOf(agent)
	task.RepositoryUrl = xptr.Ptr(repositoryURL)
	task.Branch = xptr.Ptr("agent/a1")
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
	rows[0] = paneltest.Workflow("w1", "backend")

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
		OpenedAt:          timestamppb.New(time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)),
	}
}

func newMergedPullRequest() *pullrequestv1.PullRequest {
	merged := newOpenPullRequest()
	merged.State = pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED
	merged.MergedAt = timestamppb.New(time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC))
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
func answerPushed(harness *paneltest.Panel, planned *agentv1.PlanPushResponse) {
	if planned == nil {
		return
	}

	harness.Answer(agentv1connect.AgentServicePushWorktreeProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
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
	pullRequest.Forge = forgev1.Kind_KIND_GITLAB

	return pullRequest
}

func newMergePlan() *forgev1.PlanForgePullRequestMergeResponse {
	return &forgev1.PlanForgePullRequestMergeResponse{
		Options: []*forgev1.MergeOption{
			{Method: forgev1.MergeMethod_MERGE_METHOD_MERGE},
			{
				Method:  forgev1.MergeMethod_MERGE_METHOD_SQUASH,
				Allowed: true,
				Message: &forgev1.CommitMessage{Headline: "Add the merge form (#740)", Body: "Pick a method before merging."},
			},
			{Method: forgev1.MergeMethod_MERGE_METHOD_REBASE, Allowed: true},
		},
		DefaultMethod: forgev1.MergeMethod_MERGE_METHOD_SQUASH,
		HeadSha:       "planned",
	}
}

// answerMergePlan has the forge say the pull request could merge the way the plan says.
func answerMergePlan(harness *paneltest.Panel, plan *forgev1.PlanForgePullRequestMergeResponse) {
	harness.Answer(forgev1connect.ForgeServicePlanForgePullRequestMergeProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(plan), nil
	})
}

func withMergeability(pullRequest *pullrequestv1.PullRequest, mergeability pullrequestv1.Mergeability) *pullrequestv1.PullRequest {
	pullRequest.MergeReadiness = &pullrequestv1.MergeReadiness{Mergeability: mergeability}

	return pullRequest
}
