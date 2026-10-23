package workflow

import (
	"testing"

	eventv1 "komrad/proto/gen/event/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEventPromptsComeBackWithEveryReader pins the prompts reaching every shape a workflow is read in.
func TestEventPromptsComeBackWithEveryReader(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	prompts := []EventPrompt{
		{Event: eventv1.Event_EVENT_PULL_REQUEST_CHECKS_FAILING, Prompt: "fix the checks"},
		{Event: eventv1.Event_EVENT_REVIEW_APPROVED, Prompt: "mark the pull request ready"},
		{Event: eventv1.Event_EVENT_PULL_REQUEST_BEHIND, Muted: true},
	}

	created, err := st.Create(ctx, Config{
		Name:         "backend",
		EventPrompts: prompts,
		Worktree:     WorktreeConfig{BaseBranch: "main"},
	})
	require.NoError(t, err)
	assert.Equal(t, prompts, created.EventPrompts, "create hands back what it wrote")

	got, err := st.Get(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, prompts, got.EventPrompts, "and so does a get")

	listed, err := st.List(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, prompts, listed[0].EventPrompts, "and the listing")

	page, err := st.ListPage(ctx, 10, nil, false)
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, prompts, page[0].EventPrompts, "and a page of it")
}

// TestUpdateRewritesThePromptList pins that an update replaces the whole prompt list.
func TestUpdateRewritesThePromptList(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	created, err := st.Create(ctx, Config{
		Name: "backend",
		EventPrompts: []EventPrompt{
			{Event: eventv1.Event_EVENT_PULL_REQUEST_CHECKS_FAILING, Prompt: "fix the checks"},
			{Event: eventv1.Event_EVENT_REVIEW_APPROVED, Prompt: "mark the pull request ready"},
		},
		Worktree: WorktreeConfig{BaseBranch: "main"},
	})
	require.NoError(t, err)

	kept := []EventPrompt{
		{Event: eventv1.Event_EVENT_WORKTREE_REBASE_CONFLICTED, Prompt: "settle the conflict"},
		{Event: eventv1.Event_EVENT_PULL_REQUEST_CHECKS_FAILING, Prompt: "read the failing job first"},
	}
	cfg := created.Config()
	cfg.EventPrompts = kept
	require.NoError(t, st.Update(ctx, created.ID, cfg))

	got, err := st.Get(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, kept, got.EventPrompts, "the list sent replaces the one stored, in the order sent")
}
