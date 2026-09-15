package agents

import (
	"errors"
	"testing"

	agentstore "weave/internal/store/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRetryCreateRelaunches covers a failed launch being put back into a pane.
func TestRetryCreateRelaunches(t *testing.T) {
	tmux := newFakeTmux()
	tmux.respawnErr = errors.New("bad flags")
	st := newStore(t)
	manager := newManager(tmux, st, "")

	params := NewAgentParams{
		Title:      "doomed",
		Dir:        t.TempDir(),
		Flags:      "--bad-flag",
		Prompt:     "do the thing",
		WorkflowID: seedWorkflow(t, st),
	}

	_, err := manager.launch.NewAgent(t.Context(), params)
	require.Error(t, err)

	agents, err := st.List(t.Context())
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, agentstore.StateFailedToCreate, agents[0].State)

	tmux.respawnErr = nil

	retried, err := manager.launch.RetryCreate(t.Context(), RetryCreateParams{
		ID:    agents[0].ID,
		Flags: "--good-flag",
	})
	require.NoError(t, err)
	require.NotNil(t, retried)

	got, err := st.Agents.GetByID(t.Context(), retried.ID)
	require.NoError(t, err)
	assert.Equal(t, agents[0].ID, got.ID, "the same row is reused")
	assert.Equal(t, "--good-flag", got.Flags, "flags were replaced")
	assert.NotEmpty(t, tmux.respawned, "the agent was launched into a pane")
}

// TestRetryCreateRefusesNonFailedAgent covers an agent that never failed to create.
func TestRetryCreateRefusesNonFailedAgent(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManager(tmux, st, "")

	params := NewAgentParams{
		Title:      "healthy",
		Dir:        t.TempDir(),
		Prompt:     "do the thing",
		WorkflowID: seedWorkflow(t, st),
	}

	created, err := manager.launch.NewAgent(t.Context(), params)
	require.NoError(t, err)

	_, err = manager.launch.RetryCreate(t.Context(), RetryCreateParams{ID: created.ID})
	require.ErrorIs(t, err, ErrAgentNotFailed)
}

// TestRetryCreateKeepsOriginalFlagsWhenNoneGiven covers a retry that names no flags of its own.
func TestRetryCreateKeepsOriginalFlagsWhenNoneGiven(t *testing.T) {
	tmux := newFakeTmux()
	tmux.respawnErr = errors.New("boom")
	st := newStore(t)
	manager := newManager(tmux, st, "")

	params := NewAgentParams{
		Title:      "doomed",
		Dir:        t.TempDir(),
		Flags:      "--original",
		Prompt:     "do the thing",
		WorkflowID: seedWorkflow(t, st),
	}

	_, err := manager.launch.NewAgent(t.Context(), params)
	require.Error(t, err)

	agents, err := st.List(t.Context())
	require.NoError(t, err)
	require.Len(t, agents, 1)

	tmux.respawnErr = nil

	retried, err := manager.launch.RetryCreate(t.Context(), RetryCreateParams{
		ID: agents[0].ID,
	})
	require.NoError(t, err)

	got, err := st.Agents.GetByID(t.Context(), retried.ID)
	require.NoError(t, err)
	assert.Equal(t, "--original", got.Flags, "original flags are preserved when no replacement given")
}

// TestRetryCreateFailsAgainOnBadLaunch covers a retry whose launch fails the same way.
func TestRetryCreateFailsAgainOnBadLaunch(t *testing.T) {
	tmux := newFakeTmux()
	tmux.respawnErr = errors.New("still broken")
	st := newStore(t)
	manager := newManager(tmux, st, "")

	params := NewAgentParams{
		Title:      "doomed",
		Dir:        t.TempDir(),
		Prompt:     "do the thing",
		WorkflowID: seedWorkflow(t, st),
	}

	_, err := manager.launch.NewAgent(t.Context(), params)
	require.Error(t, err)

	agents, err := st.List(t.Context())
	require.NoError(t, err)
	require.Len(t, agents, 1)
	id := agents[0].ID

	_, err = manager.launch.RetryCreate(t.Context(), RetryCreateParams{ID: id})
	require.Error(t, err)

	got, err := st.Agents.GetByID(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, agentstore.StateFailedToCreate, got.State, "goes back to failed_to_create for another retry")
}
