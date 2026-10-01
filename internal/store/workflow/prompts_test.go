package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPromptsComeBackWithEveryReader pins the prompts reaching every shape a workflow is read in.
func TestPromptsComeBackWithEveryReader(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	prompts := []Prompt{
		{Notice: "pull_request.checks_failing", Prompt: "fix the checks"},
		{Notice: "review.approved", Prompt: "mark the pull request ready"},
	}

	created, err := st.Create(ctx, Config{
		Name:     "backend",
		Prompts:  prompts,
		Worktree: WorktreeConfig{BaseBranch: "main"},
	})
	require.NoError(t, err)
	assert.Equal(t, prompts, created.Prompts, "create hands back what it wrote")

	got, err := st.Get(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, prompts, got.Prompts, "and so does a get")

	listed, err := st.List(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, prompts, listed[0].Prompts, "and the listing")

	page, err := st.ListPage(ctx, 10, nil, false)
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, prompts, page[0].Prompts, "and a page of it")
}

// TestUpdateRewritesThePromptList pins that an update replaces the whole prompt list.
func TestUpdateRewritesThePromptList(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	created, err := st.Create(ctx, Config{
		Name: "backend",
		Prompts: []Prompt{
			{Notice: "pull_request.checks_failing", Prompt: "fix the checks"},
			{Notice: "review.approved", Prompt: "mark the pull request ready"},
		},
		Worktree: WorktreeConfig{BaseBranch: "main"},
	})
	require.NoError(t, err)

	kept := []Prompt{
		{Notice: "worktree.rebase_conflicted", Prompt: "settle the conflict"},
		{Notice: "pull_request.checks_failing", Prompt: "read the failing job first"},
	}
	cfg := created.Config()
	cfg.Prompts = kept
	require.NoError(t, st.Update(ctx, created.ID, cfg))

	got, err := st.Get(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, kept, got.Prompts, "the list sent replaces the one stored, in the order sent")

	assert.Equal(t, "settle the conflict", got.GetPrompt("worktree.rebase_conflicted"))
	assert.Empty(t, got.GetPrompt("review.approved"), "a notice dropped from the list has no prompt")
}
