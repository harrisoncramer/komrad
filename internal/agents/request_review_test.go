package agents

import (
	"testing"

	"komrad/internal/harness"
	"komrad/internal/harnesstest"
	agentstore "komrad/internal/store/agent"
	reviewstore "komrad/internal/store/review"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/tmuxtest"
	"komrad/lib/xptr"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRequestReviewSpawnsTheReviewer pins what asking for a review makes.
func TestRequestReviewSpawnsTheReviewer(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager, fakeHarness := newManagerWithHarness(tmux, st, "")
	ctx := t.Context()

	dir := t.TempDir()
	author, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "author",
		Dir:        dir,
		Prompt:     "write the change",
		WorkflowID: seedWorkflow(t, st),
	})
	require.NoError(t, err)

	reviewer, guidance, err := manager.launch.RequestReview(ctx, RequestReviewParams{
		AgentID: author.ID,
		Prompt:  "it rewrites the parser; the lookahead is where I am unsure",
	})
	require.NoError(t, err)
	require.NotNil(t, reviewer)

	assert.Equal(t, agentstore.RoleReviewer, reviewer.Role, "so it is briefed to read rather than to write")
	assert.Equal(t, "author-reviewer", reviewer.Name, "named after the work it reads")
	require.NotNil(t, reviewer.ParentAgentID)
	assert.Equal(t, author.ID, *reviewer.ParentAgentID, "a reviewer may only review its parent")
	assert.Equal(t, dir, reviewer.Cwd, "and reads that parent's checkout rather than one of its own")
	assert.Equal(t, author.WorkflowID, reviewer.WorkflowID)

	assert.Contains(t, reviewer.Prompt, "it rewrites the parser", "what the asker said reaches it")
	assert.Contains(t, reviewer.Prompt, author.ID, "and so does whose work it is")

	assert.Contains(t, guidance, "Settle everything it raises",
		"the asker is handed the rest of the loop, since it was not launched with this half")

	require.Len(t, fakeHarness.Launches, 2)
	briefing := fakeHarness.Launches[1].Context
	assert.Contains(t, briefing, "### When you are the one reviewing",
		"the role written on the row is what decides the half the pane is briefed with")
	assert.NotContains(t, briefing, "### Getting your own work read")
}

// TestRequestReviewNamesTheBranch pins the one fact komrad holds that an agent asking for a
// review tends to leave out of its own prompt.
func TestRequestReviewNamesTheBranch(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := newManager(tmux, st, "")
	ctx := t.Context()

	author, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "author",
		Dir:        t.TempDir(),
		Prompt:     "write the change",
		WorkflowID: seedWorkflow(t, st),
	})
	require.NoError(t, err)
	require.NoError(t, st.Agents.SetCheckout(ctx, author.ID, agentstore.SetCheckoutParams{Branch: xptr.Ptr("agent/one")}))

	reviewer, _, err := manager.launch.RequestReview(ctx, RequestReviewParams{AgentID: author.ID, Prompt: "read it"})
	require.NoError(t, err)

	assert.Contains(t, reviewer.Prompt, "agent/one")
}

// TestRequestReviewTitleOverridesTheName pins that a caller which named the reviewer keeps that
// name.
func TestRequestReviewTitleOverridesTheName(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := newManager(tmux, st, "")
	ctx := t.Context()

	author, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "author",
		Dir:        t.TempDir(),
		Prompt:     "write the change",
		WorkflowID: seedWorkflow(t, st),
	})
	require.NoError(t, err)

	reviewer, _, err := manager.launch.RequestReview(ctx, RequestReviewParams{
		AgentID: author.ID,
		Title:   "second-reader",
		Prompt:  "read it",
	})
	require.NoError(t, err)
	assert.Equal(t, "second-reader", reviewer.Name)
}

// TestRequestReviewRefusals pins the agents that have no work of their own to be read.
func TestRequestReviewRefusals(t *testing.T) {
	tests := []struct {
		name      string
		agentID   func(author, reviewer agentstore.Agent) string
		wantErrIs error
	}{
		{
			name:      "a reviewer asking for a review of the reading it did",
			agentID:   func(_, reviewer agentstore.Agent) string { return reviewer.ID },
			wantErrIs: ErrReviewNeedsOwnWork,
		},
		{
			name:      "an agent nothing answers to",
			agentID:   func(_, _ agentstore.Agent) string { return "b9b1a1de-0000-7000-8000-000000000000" },
			wantErrIs: ErrAgentNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			st := newStore(t)
			manager := newManager(tmux, st, "")
			ctx := t.Context()

			author, err := manager.launch.NewAgent(ctx, NewAgentParams{
				Title:      "author",
				Dir:        t.TempDir(),
				Prompt:     "write the change",
				WorkflowID: seedWorkflow(t, st),
			})
			require.NoError(t, err)

			reviewer, _, err := manager.launch.RequestReview(ctx, RequestReviewParams{AgentID: author.ID, Prompt: "read it"})
			require.NoError(t, err)

			_, _, err = manager.launch.RequestReview(ctx, RequestReviewParams{
				AgentID: tt.agentID(*author, *reviewer),
				Prompt:  "read it",
			})
			require.ErrorIs(t, err, tt.wantErrIs)
		})
	}
}

// TestRequestReviewKeepsOneReviewer pins that a second ask is refused rather than answered with
// a second reviewer.
func TestRequestReviewKeepsOneReviewer(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := newManager(tmux, st, "")
	ctx := t.Context()

	author, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "author",
		Dir:        t.TempDir(),
		Prompt:     "write the change",
		WorkflowID: seedWorkflow(t, st),
	})
	require.NoError(t, err)

	reviewer, _, err := manager.launch.RequestReview(ctx, RequestReviewParams{AgentID: author.ID, Prompt: "read it"})
	require.NoError(t, err)

	_, _, err = manager.launch.RequestReview(ctx, RequestReviewParams{AgentID: author.ID, Prompt: "read it again"})
	require.ErrorIs(t, err, ErrReviewerAlreadyRunning)
	assert.Contains(t, err.Error(), reviewer.Name, "so the asker is told which agent is already reading")

	children, err := st.Agents.ListChildren(ctx, []string{author.ID})
	require.NoError(t, err)
	assert.Len(t, children, 1, "and no second reviewer was spawned")

	require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, []string{reviewer.ID}))

	second, _, err := manager.launch.RequestReview(ctx, RequestReviewParams{AgentID: author.ID, Prompt: "read it"})
	require.NoError(t, err, "an agent whose reviewer is gone may have another")
	assert.NotEqual(t, reviewer.ID, second.ID)
}

// TestRequestReviewRetryAnswersWithTheSameReviewer pins that a spent idempotency key is still a
// retry rather than a second ask.
func TestRequestReviewRetryAnswersWithTheSameReviewer(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := newManager(tmux, st, "")
	ctx := t.Context()

	author, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "author",
		Dir:        t.TempDir(),
		Prompt:     "write the change",
		WorkflowID: seedWorkflow(t, st),
	})
	require.NoError(t, err)

	first, _, err := manager.launch.RequestReview(ctx, RequestReviewParams{
		AgentID:        author.ID,
		Prompt:         "read it",
		IdempotencyKey: "abc",
	})
	require.NoError(t, err)

	retried, guidance, err := manager.launch.RequestReview(ctx, RequestReviewParams{
		AgentID:        author.ID,
		Prompt:         "read it",
		IdempotencyKey: "abc",
	})
	require.NoError(t, err)
	assert.Equal(t, first.ID, retried.ID)
	assert.NotEmpty(t, guidance, "a retry is answered with everything the call it repeats answered with")

	_, _, err = manager.launch.RequestReview(ctx, RequestReviewParams{
		AgentID:        author.ID,
		Prompt:         "read it",
		IdempotencyKey: "def",
	})
	require.ErrorIs(t, err, ErrReviewerAlreadyRunning, "a different key is a second ask rather than a retry")
}

// TestRequestReviewAfterAnApprovalSpawnsAnother pins that the guard holds a change to one
// reviewer rather than an agent to one for its whole life.
func TestRequestReviewAfterAnApprovalSpawnsAnother(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := newManager(tmux, st, "")
	reviews := reviewstore.NewReviewStore(st.Pool.GetDB())
	ctx := t.Context()

	author, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "author",
		Dir:        t.TempDir(),
		Prompt:     "write the change",
		WorkflowID: seedWorkflow(t, st),
	})
	require.NoError(t, err)

	first, _, err := manager.launch.RequestReview(ctx, RequestReviewParams{AgentID: author.ID, Prompt: "read it"})
	require.NoError(t, err)

	round, err := reviews.CreateRound(ctx, reviewstore.CreateRoundParams{
		AgentID:     author.ID,
		BaseSHA:     "base-sha",
		SnapshotSHA: "snapshot-sha",
	})
	require.NoError(t, err)

	open, err := reviews.OpenReview(ctx, reviewstore.OpenReviewParams{
		AgentID:       author.ID,
		RoundID:       round.ID,
		AuthorKind:    reviewstore.AuthorAgent,
		AuthorAgentID: &first.ID,
	})
	require.NoError(t, err)

	_, _, err = manager.launch.RequestReview(ctx, RequestReviewParams{AgentID: author.ID, Prompt: "read it"})
	require.ErrorIs(t, err, ErrReviewerAlreadyRunning, "an unreleased review is a reviewer still reading")

	_, err = reviews.ReleaseReview(ctx, reviewstore.ReleaseReviewParams{
		ID:      open.ID,
		Verdict: reviewstore.VerdictApproved,
	})
	require.NoError(t, err)

	second, _, err := manager.launch.RequestReview(ctx, RequestReviewParams{AgentID: author.ID, Prompt: "read the next one"})
	require.NoError(t, err, "a reviewer that approved has said everything it has to say")
	assert.NotEqual(t, first.ID, second.ID)
}

// TestRequestReviewRefusesAChild pins that an ordinary child is refused too, however it was
// briefed.
func TestRequestReviewRefusesAChild(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := newManager(tmux, st, "")
	ctx := t.Context()

	parent, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "author",
		Dir:        t.TempDir(),
		Prompt:     "write the change",
		WorkflowID: seedWorkflow(t, st),
	})
	require.NoError(t, err)

	child, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:         "runner",
		Prompt:        "run the tests",
		WorkflowID:    parent.WorkflowID,
		ParentAgentID: parent.ID,
	})
	require.NoError(t, err)

	_, _, err = manager.launch.RequestReview(ctx, RequestReviewParams{AgentID: child.ID, Prompt: "read it"})
	require.ErrorIs(t, err, ErrReviewNeedsOwnWork)
}

// TestWorktreeAgentIsToldToGetItsWorkRead pins the delivery of the other half, which reaches the
// agents komrad cut a checkout for and no others.
func TestWorktreeAgentIsToldToGetItsWorkRead(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager, fakeHarness := newManagerWithHarness(tmux, st, "")
	ctx := t.Context()

	workflow, err := st.Workflows.Create(ctx, worktreeConfig(t, "worktrees"))
	require.NoError(t, err)

	author, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "author",
		Prompt:     "write the change",
		WorkflowID: workflow.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, agentstore.RoleImplementer, author.Role, "an agent that names no role writes rather than reads")

	require.Len(t, fakeHarness.Launches, 1)
	briefing := fakeHarness.Launches[0].Context
	assert.Contains(t, briefing, "### Getting your own work read")
	assert.Contains(t, briefing, "Commit on your branch and stop there")
	assert.NotContains(t, briefing, "### When you are the one reviewing")
}

// TestRequestReviewIsNotCaughtByTheOrchestratorRefusal pins that the ordinary use of a child still
// stands.
func TestRequestReviewIsNotCaughtByTheOrchestratorRefusal(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := newManager(tmux, st, "")
	ctx := t.Context()

	author, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "author",
		Dir:        t.TempDir(),
		Prompt:     "write the change",
		WorkflowID: seedWorkflow(t, st),
	})
	require.NoError(t, err)

	reviewer, _, err := manager.launch.RequestReview(ctx, RequestReviewParams{
		AgentID: author.ID,
		Prompt:  "read the parser change",
	})
	require.NotErrorIs(t, err, ErrParentIsOrchestrator)
	require.NoError(t, err)
	require.NotNil(t, reviewer)
	require.NotNil(t, reviewer.ParentAgentID)
	assert.Equal(t, author.ID, *reviewer.ParentAgentID)
}

// TestRequestReviewRefusesAnOrchestratorBeforeItSpawns pins that an orchestrator asking for a
// review is turned away by the role check rather than by the parent check.
func TestRequestReviewRefusesAnOrchestratorBeforeItSpawns(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := newManager(tmux, st, "")
	ctx := t.Context()

	author := seedOrchestrator(t, st, tmux)

	_, _, err := manager.launch.RequestReview(ctx, RequestReviewParams{AgentID: author.ID, Prompt: "read it"})
	require.ErrorIs(t, err, ErrReviewNeedsOwnWork)
}

// TestRequestReviewLandsTheReviewerOnTheHarnessAsked pins which CLI the reviewer runs, taking
// what the asker named ahead of the workflow's default.
func TestRequestReviewLandsTheReviewerOnTheHarnessAsked(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		workflow  string
		want      string
	}{
		{
			name: "neither the asker nor the workflow names one, so it takes the configured default",
			want: harnesstest.FakeID,
		},
		{
			name:     "the workflow's default when the asker names none",
			workflow: "codex",
			want:     "codex",
		},
		{
			name:      "the one asked for, over a workflow that defaults to another",
			requested: "codex",
			workflow:  "gemini",
			want:      "codex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := tmuxtest.NewFake()
			byID := map[string]*harnesstest.Fake{
				harnesstest.FakeID: {Name: harnesstest.FakeID},
				"codex":            {Name: "codex", NoSession: true},
				"gemini":           {Name: "gemini"},
			}
			service := st.agentService(agentServiceOptions{
				tmux:      tmux,
				harnesses: harnesstest.Registry{ByID: byID},
			})
			ctx := t.Context()

			created, err := st.Workflows.Create(ctx, workflowstore.Config{
				Name:     "workflow-" + uuid.NewString(),
				Harness:  tt.workflow,
				Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"},
			})
			require.NoError(t, err)

			author, err := service.launch.NewAgent(ctx, NewAgentParams{
				Title:      "author",
				Dir:        t.TempDir(),
				Prompt:     "write the change",
				Harness:    harnesstest.FakeID,
				WorkflowID: created.ID,
			})
			require.NoError(t, err)

			reviewer, _, err := service.launch.RequestReview(ctx, RequestReviewParams{
				AgentID: author.ID,
				Prompt:  "read it",
				Harness: tt.requested,
			})
			require.NoError(t, err)

			assert.Equal(t, tt.want, reviewer.Harness)
			assert.Equal(t, agentstore.RoleReviewer, reviewer.Role, "a different CLI is still a reviewer")
			require.NotNil(t, reviewer.ParentAgentID)
			assert.Equal(t, author.ID, *reviewer.ParentAgentID, "and still the asker's child")
			assert.Equal(t, author.Cwd, reviewer.Cwd, "reading the asker's checkout")

			launches := byID[tt.want].Launches
			require.NotEmpty(t, launches, "the CLI named on the row is the one launched")
			assert.Contains(t, launches[len(launches)-1].Context, "### When you are the one reviewing")
		})
	}
}

// TestRequestReviewRefusesUnknownHarness pins that a CLI this binary cannot run is refused the
// way a create is, before a reviewer row is written.
func TestRequestReviewRefusesUnknownHarness(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	service := st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}, Only: []string{harnesstest.FakeID}},
	})
	ctx := t.Context()

	author, err := service.launch.NewAgent(ctx, NewAgentParams{
		Title:      "author",
		Dir:        t.TempDir(),
		Prompt:     "write the change",
		WorkflowID: seedWorkflow(t, st),
	})
	require.NoError(t, err)

	_, _, err = service.launch.RequestReview(ctx, RequestReviewParams{
		AgentID: author.ID,
		Prompt:  "read it",
		Harness: "gemini",
	})
	require.ErrorIs(t, err, harness.ErrNotRegistered)

	all, err := st.Agents.List(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 1, "a refused harness records no reviewer")
	assert.Len(t, tmux.NewWindows, 1, "and opens no second window")
}

// TestRequestReviewFlagsReachTheLaunch pins that the flags an asker gives its reviewer compose
// the way they do on a create, the user's defaults for the CLI ahead of them.
func TestRequestReviewFlagsReachTheLaunch(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager, fakeHarness := newManagerWithHarness(tmux, st, "--model opus")
	ctx := t.Context()

	author, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "author",
		Dir:        t.TempDir(),
		Prompt:     "write the change",
		WorkflowID: seedWorkflow(t, st),
	})
	require.NoError(t, err)

	reviewer, _, err := manager.launch.RequestReview(ctx, RequestReviewParams{
		AgentID: author.ID,
		Prompt:  "read it",
		Flags:   "--effort high",
	})
	require.NoError(t, err)

	assert.Equal(t, "--model opus --effort high", reviewer.Flags)
	require.Len(t, fakeHarness.Launches, 2)
	assert.Equal(t, "--model opus --effort high", fakeHarness.Launches[1].Flags)
}
