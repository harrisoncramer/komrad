package ui_test

import (
	"testing"

	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type chatCase struct {
	name   string
	role   agentv1.AgentRole
	params panelHarnessParams
	guest  string
}

// TestReviewChat covers the implementer's chat under the review, when it is up and how the keys reach it.
func TestReviewChat(t *testing.T) {
	tests := []chatCase{
		{
			name:   "a reviewer's chat stays in its own window",
			role:   agentv1.AgentRole_AGENT_ROLE_REVIEWER,
			params: panelHarnessParams{},
			guest:  "",
		},
		{
			name:   "a window too short for the diff and the chat leaves the chat out",
			role:   agentv1.AgentRole_AGENT_ROLE_IMPLEMENTER,
			params: panelHarnessParams{height: 24},
			guest:  "",
		},
		{
			name:   "the chat switched off in the config stays out",
			role:   agentv1.AgentRole_AGENT_ROLE_IMPLEMENTER,
			params: panelHarnessParams{hideChat: true},
			guest:  "",
		},
		{
			name:   "an implementer's chat comes in under the diff",
			role:   agentv1.AgentRole_AGENT_ROLE_IMPLEMENTER,
			params: panelHarnessParams{},
			guest:  "%9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newReviewServer(t)
			server.agent.Role = tt.role
			harness := openReviewAt(t, server, tt.params)

			assert.Equal(t, tt.guest, harness.panel.ReadTileGuest())
		})
	}

	t.Run("the leader steps from the diff into the chat", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.Role = agentv1.AgentRole_AGENT_ROLE_IMPLEMENTER
		harness := openReview(t, server)
		require.Equal(t, "%9", harness.panel.ReadTileGuest())

		harness.press("ctrl+space", "ctrl+space")

		assert.Equal(t, []string{"%9"}, harness.panel.ReadSelectedPanes())
	})

	t.Run("leaving the review sends the chat home", func(t *testing.T) {
		server := newReviewServer(t)
		server.agent.Role = agentv1.AgentRole_AGENT_ROLE_IMPLEMENTER
		harness := openReview(t, server)
		require.Equal(t, "%9", harness.panel.ReadTileGuest())

		harness.press("esc")

		assert.Empty(t, harness.panel.ReadTileGuest())
		assert.Contains(t, harness.panel.ReadKilledPanes(), "%3")
	})
}
