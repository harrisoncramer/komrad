package agents

import (
	"testing"

	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRequestReviewSpawnsTheReviewer pins what asking for a review makes, which is the child
// weave would otherwise be relying on an agent to describe correctly for itself.
func TestRequestReviewSpawnsTheReviewer(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager, h := newManagerWithHarness(tmux, st, "")
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

	require.Len(t, h.launches, 2)
	briefing := h.launches[1].Context
	assert.Contains(t, briefing, "### When you are the one reviewing",
		"the role written on the row is what decides the half the pane is briefed with")
	assert.NotContains(t, briefing, "### Getting your own work read")
}

// TestRequestReviewNamesTheBranch pins the one fact weave holds that an agent asking for a
// review tends to leave out of its own prompt.
func TestRequestReviewNamesTheBranch(t *testing.T) {
	tmux := newFakeTmux()
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
	require.NoError(t, st.Agents.SetBranchByID(ctx, author.ID, xptr.Ptr("agent/one")))

	reviewer, _, err := manager.launch.RequestReview(ctx, RequestReviewParams{AgentID: author.ID, Prompt: "read it"})
	require.NoError(t, err)

	assert.Contains(t, reviewer.Prompt, "agent/one")
}

// TestRequestReviewTitleOverridesTheName pins that a caller which named the reviewer keeps that
// name.
func TestRequestReviewTitleOverridesTheName(t *testing.T) {
	tmux := newFakeTmux()
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
			tmux := newFakeTmux()
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
	tmux := newFakeTmux()
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
	tmux := newFakeTmux()
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
	assert.ErrorIs(t, err, ErrReviewerAlreadyRunning, "a different key is a second ask rather than a retry")
}

// TestRequestReviewAfterAnApprovalSpawnsAnother pins that the guard holds a change to one
// reviewer rather than an agent to one for its whole life.
func TestRequestReviewAfterAnApprovalSpawnsAnother(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManager(tmux, st, "")
	reviews := reviewstore.New(st.Pool.GetDB())
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
	tmux := newFakeTmux()
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
// agents weave cut a checkout for and no others.
func TestWorktreeAgentIsToldToGetItsWorkRead(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager, h := newManagerWithHarness(tmux, st, "")
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

	require.Len(t, h.launches, 1)
	briefing := h.launches[0].Context
	assert.Contains(t, briefing, "### Getting your own work read")
	assert.Contains(t, briefing, "Commit on your branch and stop there")
	assert.NotContains(t, briefing, "### When you are the one reviewing")
}
