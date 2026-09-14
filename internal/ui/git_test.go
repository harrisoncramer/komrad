package ui

import (
	"testing"

	"weave/config"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gitModel is the agents list with the cursor on an agent whose workflow makes worktrees.
func gitModel(t *testing.T) model {
	t.Helper()

	m := infoModel(&recordingTmux{width: 200, height: 40})
	m.width, m.height = 200, 40
	m.api = driftAPI()
	m.listing.workflows = []workflowstore.Workflow{{
		ID:       "w1",
		Name:     "backend",
		Worktree: workflowstore.WorktreeConfig{Enabled: true},
	}}
	m.listing.agents[0].agent.Metadata = []agentstore.Metadata{{Key: prMetadataKey, Value: "https://example.test/pr/1"}}

	return m
}

func routeKey(t *testing.T, m model, pressed string) model {
	t.Helper()

	next, _ := m.route(keyMsgOf(t, pressed))
	updated, ok := next.(model)
	require.True(t, ok)

	return updated
}

// TestGitMenuOpensOnAnAgent covers the key that draws the menu, and the row it does nothing on.
func TestGitMenuOpensOnAnAgent(t *testing.T) {
	opened := routeKey(t, gitModel(t), "b")
	assert.Equal(t, formGit, opened.form.kind)

	onWorkflow := gitModel(t)
	onWorkflow.tree.cursor = 0
	assert.Equal(t, formNone, routeKey(t, onWorkflow, "b").form.kind,
		"a workflow header has no branch of its own")
}

// TestGitMenuClosesOnEsc covers leaving the menu without picking anything.
func TestGitMenuClosesOnEsc(t *testing.T) {
	closed := routeKey(t, routeKey(t, gitModel(t), "b"), "esc")

	assert.Equal(t, formNone, closed.form.kind)
	assert.Empty(t, closed.messages.items)
}

// TestGitMenuEntriesAnswerTheirOwnKeys covers picking an entry by its letter rather than by
// walking the cursor onto it.
func TestGitMenuEntriesAnswerTheirOwnKeys(t *testing.T) {
	for _, pressed := range []string{"r", "o"} {
		t.Run(pressed, func(t *testing.T) {
			picked := routeKey(t, routeKey(t, gitModel(t), "b"), pressed)

			assert.Equal(t, formNone, picked.form.kind)
			assert.Empty(t, picked.messages.items, "the entry answered rather than saying why it could not")
		})
	}
}

// TestGitMenuSaysWhyAnEntryWouldDoNothing covers an agent neither entry can act on, where the
// menu is still drawn and the reason is read rather than the press being swallowed.
func TestGitMenuSaysWhyAnEntryWouldDoNothing(t *testing.T) {
	tests := []struct {
		name    string
		pressed string
		want    string
	}{
		{name: "the rebase", pressed: "r", want: errNoWorktree.Error()},
		{name: "the pull request", pressed: "o", want: errNoPR.Error()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := gitModel(t)
			m.listing.workflows[0].Worktree.Enabled = false
			m.listing.agents[0].agent.Metadata = nil

			opened := routeKey(t, m, "b")
			require.Equal(t, formGit, opened.form.kind)

			picked := routeKey(t, opened, tt.pressed)

			assert.Equal(t, formNone, picked.form.kind)
			require.Len(t, picked.messages.items, 1)
			assert.Contains(t, picked.messages.items[0].detail, tt.want)
		})
	}
}

// TestGitMenuEntriesNameTheirKeysAndTheirRefusals covers the labels, which carry both the letter
// that picks an entry and the reason it would do nothing on this agent.
func TestGitMenuEntriesNameTheirKeysAndTheirRefusals(t *testing.T) {
	entry := gitEntry{
		name:    "Rebase onto the base",
		keys:    config.DefaultKeybindings().Git.Rebase,
		refusal: "no worktree of its own",
		held:    true,
	}

	assert.Equal(t, "Rebase onto the base (r)", gitEntryLabel(entry))

	entry.held = false
	assert.Equal(t, "Rebase onto the base (r), no worktree of its own", gitEntryLabel(entry))

	entry.keys = nil
	assert.Equal(t, "Rebase onto the base, no worktree of its own", gitEntryLabel(entry))
}

// TestGitMenuDrawsEveryEntry covers the menu on an agent neither entry can act on, where the
// select would otherwise scroll the first of them out of the column.
func TestGitMenuDrawsEveryEntry(t *testing.T) {
	m := gitModel(t)
	m.listing.workflows[0].Worktree.Enabled = false
	m.listing.agents[0].agent.Metadata = nil

	drawn := routeKey(t, m, "b").View()

	assert.Contains(t, drawn, "Rebase onto the base")
	assert.Contains(t, drawn, "Open the pull request")
	assert.Contains(t, drawn, "Cancel")
}
