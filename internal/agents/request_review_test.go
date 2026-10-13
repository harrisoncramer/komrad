package agents

import (
	"testing"

	"komrad/config"
	"komrad/internal/agenterr"
	"komrad/internal/caller"
	"komrad/internal/harness"
	"komrad/internal/harnesstest"
	agentstore "komrad/internal/store/agent"
	taskstore "komrad/internal/store/task"
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
	require.NotNil(t, reviewer.TaskID)
	assert.Equal(t, agentByID(t, st, author.ID).TaskID, reviewer.TaskID, "a reviewer reads its writer's task")
	assert.Equal(t, dir, reviewer.Cwd, "and reads its writer's checkout rather than one of its own")
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
	manager := newManager(tmux, st)
	ctx := t.Context()

	author, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "author",
		Dir:        t.TempDir(),
		Prompt:     "write the change",
		WorkflowID: seedWorkflow(t, st),
	})
	require.NoError(t, err)
	st.SeedTaskHead(t, author.ID, taskstore.SetHeadParams{Branch: xptr.Ptr("agent/one")})

	reviewer, _, err := manager.launch.RequestReview(ctx, RequestReviewParams{AgentID: author.ID, Prompt: "read it"})
	require.NoError(t, err)

	assert.Contains(t, reviewer.Prompt, "agent/one")
}

// TestRequestReviewTitleOverridesTheName pins that a caller which named the reviewer keeps that
// name.
func TestRequestReviewTitleOverridesTheName(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := newManager(tmux, st)
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
			wantErrIs: agenterr.ErrReviewNeedsOwnWork,
		},
		{
			name:      "an agent nothing answers to",
			agentID:   func(_, _ agentstore.Agent) string { return "b9b1a1de-0000-7000-8000-000000000000" },
			wantErrIs: agenterr.ErrAgentNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			st := newStore(t)
			manager := newManager(tmux, st)
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

// TestRequestReviewUnderTheReviewerCeiling pins that a writer may ask for another pass while one
// is reading, up to the reviewers its task may hold.
func TestRequestReviewUnderTheReviewerCeiling(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}},
		addr:      "127.0.0.1:9999",
		usage:     config.Usage{Task: config.TaskUsage{MaxReviewers: 2}},
	})
	ctx := t.Context()

	author, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "author",
		Dir:        t.TempDir(),
		Prompt:     "write the change",
		WorkflowID: seedWorkflow(t, st),
	})
	require.NoError(t, err)

	first, _, err := manager.launch.RequestReview(ctx, RequestReviewParams{AgentID: author.ID, Prompt: "correctness pass"})
	require.NoError(t, err)

	second, _, err := manager.launch.RequestReview(ctx, RequestReviewParams{AgentID: author.ID, Prompt: "refactor pass"})
	require.NoError(t, err, "a second pass while the first is still reading")
	assert.NotEqual(t, first.ID, second.ID)

	_, _, err = manager.launch.RequestReview(ctx, RequestReviewParams{AgentID: author.ID, Prompt: "a third pass"})
	require.ErrorIs(t, err, agenterr.ErrTooManyReviewers)

	reviewers, err := st.Agents.CountLiveReviewers(ctx, *agentByID(t, st, author.ID).TaskID)
	require.NoError(t, err)
	assert.Equal(t, 2, reviewers)
}

// TestRequestReviewRetryAnswersWithTheSameReviewer pins that a spent idempotency key is still a
// retry rather than a second ask.
func TestRequestReviewRetryAnswersWithTheSameReviewer(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := newManager(tmux, st)
	ctx := t.Context()

	author, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "author",
		Dir:        t.TempDir(),
		Prompt:     "write the change",
		WorkflowID: seedWorkflow(t, st),
	})
	require.NoError(t, err)
	authorCtx := caller.With(ctx, caller.Caller{AgentID: author.ID})

	first, _, err := manager.launch.RequestReview(authorCtx, RequestReviewParams{
		AgentID:        author.ID,
		Prompt:         "read it",
		IdempotencyKey: "abc",
	})
	require.NoError(t, err)

	retried, guidance, err := manager.launch.RequestReview(authorCtx, RequestReviewParams{
		AgentID:        author.ID,
		Prompt:         "read it",
		IdempotencyKey: "abc",
	})
	require.NoError(t, err)
	assert.Equal(t, first.ID, retried.ID)
	assert.NotEmpty(t, guidance, "a retry is answered with everything the call it repeats answered with")

	second, _, err := manager.launch.RequestReview(authorCtx, RequestReviewParams{
		AgentID:        author.ID,
		Prompt:         "read it",
		IdempotencyKey: "def",
	})
	require.NoError(t, err)
	assert.NotEqual(t, first.ID, second.ID, "a different key is a second ask rather than a retry")
}

// TestRequestReviewKeyIsScopedToCaller pins that one key from another caller is a new ask.
func TestRequestReviewKeyIsScopedToCaller(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := newManager(tmux, st)
	ctx := t.Context()

	ids := map[string]string{}
	for _, name := range []string{"author", "other author"} {
		author, err := manager.launch.NewAgent(ctx, NewAgentParams{
			Title:      name,
			Dir:        t.TempDir(),
			Prompt:     "write the change",
			WorkflowID: seedWorkflow(t, st),
		})
		require.NoError(t, err)
		ids[name] = author.ID
	}

	first, _, err := manager.launch.RequestReview(caller.With(ctx, caller.Caller{AgentID: ids["author"]}), RequestReviewParams{
		AgentID:        ids["author"],
		Prompt:         "read it",
		IdempotencyKey: "abc",
	})
	require.NoError(t, err)

	second, _, err := manager.launch.RequestReview(caller.With(ctx, caller.Caller{AgentID: ids["other author"]}), RequestReviewParams{
		AgentID:        ids["other author"],
		Prompt:         "read it",
		IdempotencyKey: "abc",
	})
	require.NoError(t, err)
	assert.NotEqual(t, first.ID, second.ID)
	assert.Equal(t, agentByID(t, st, ids["other author"]).TaskID, second.TaskID)
}

// TestRequestReviewRefusesAKeySpentOnAnotherAgent pins that a key one caller already spent on an
// agent other than this author's reviewer is refused rather than answered with that agent.
func TestRequestReviewRefusesAKeySpentOnAnotherAgent(t *testing.T) {
	tests := []struct {
		name  string
		spend func(t *testing.T, launch *LaunchService, ids map[string]string)
		asker string
	}{
		{
			name: "a key spent on a spawn of the author's own",
			spend: func(t *testing.T, launch *LaunchService, ids map[string]string) {
				_, err := launch.NewAgent(caller.With(t.Context(), caller.Caller{AgentID: ids["author"]}), NewAgentParams{
					Title:          "side work",
					Dir:            t.TempDir(),
					Prompt:         "look into it",
					WorkflowID:     ids["workflow"],
					IdempotencyKey: "abc",
				})
				require.NoError(t, err)
			},
			asker: "author",
		},
		{
			name: "one caller's key on two authors",
			spend: func(t *testing.T, launch *LaunchService, ids map[string]string) {
				_, _, err := launch.RequestReview(t.Context(), RequestReviewParams{
					AgentID:        ids["other author"],
					Prompt:         "read it",
					IdempotencyKey: "abc",
				})
				require.NoError(t, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			st := newStore(t)
			manager := newManager(tmux, st)
			ctx := t.Context()

			ids := map[string]string{"workflow": seedWorkflow(t, st)}
			for _, name := range []string{"author", "other author"} {
				author, err := manager.launch.NewAgent(ctx, NewAgentParams{
					Title:      name,
					Dir:        t.TempDir(),
					Prompt:     "write the change",
					WorkflowID: ids["workflow"],
				})
				require.NoError(t, err)
				ids[name] = author.ID
			}
			tt.spend(t, manager.launch, ids)

			_, _, err := manager.launch.RequestReview(caller.With(ctx, caller.Caller{AgentID: ids[tt.asker]}), RequestReviewParams{
				AgentID:        ids["author"],
				Prompt:         "read it",
				IdempotencyKey: "abc",
			})
			require.ErrorIs(t, err, errKeySpentElsewhere)
		})
	}
}

// TestRequestReviewRefusesAReviewer pins that a reviewer spawned onto a task is refused, however
// it was briefed.
func TestRequestReviewRefusesAReviewer(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := newManager(tmux, st)
	ctx := t.Context()

	writer, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "author",
		Dir:        t.TempDir(),
		Prompt:     "write the change",
		WorkflowID: seedWorkflow(t, st),
	})
	require.NoError(t, err)

	reviewer, err := manager.launch.NewAgent(ctx, reviewerOf(agentByID(t, st, writer.ID), "runner"))
	require.NoError(t, err)

	_, _, err = manager.launch.RequestReview(ctx, RequestReviewParams{AgentID: reviewer.ID, Prompt: "read it"})
	require.ErrorIs(t, err, agenterr.ErrReviewNeedsOwnWork)
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
	assert.Equal(t, agentstore.RoleWriter, author.Role, "an agent that names no role writes rather than reads")

	require.Len(t, fakeHarness.Launches, 1)
	briefing := fakeHarness.Launches[0].Context
	assert.Contains(t, briefing, "### Getting your own work read")
	assert.Contains(t, briefing, "Commit on your branch, then call request_review")
	assert.NotContains(t, briefing, "### When you are the one reviewing")
}

// TestRequestReviewRefusesAnOrchestratorBeforeItSpawns pins that an orchestrator asking for a
// review is turned away before a reviewer is spawned.
func TestRequestReviewRefusesAnOrchestratorBeforeItSpawns(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := newManager(tmux, st)
	ctx := t.Context()

	author := seedOrchestrator(t, st, tmux)

	_, _, err := manager.launch.RequestReview(ctx, RequestReviewParams{AgentID: author.ID, Prompt: "read it"})
	require.ErrorIs(t, err, agenterr.ErrReviewNeedsOwnWork)
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
				"codex":            {Name: "codex", Naming: harness.SessionDiscovered},
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
			assert.Equal(t, agentByID(t, st, author.ID).TaskID, reviewer.TaskID, "and still on the asker's task")
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
