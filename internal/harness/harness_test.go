package harness

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestEnvPrefix covers what goes ahead of the CLI, the tools directory among it so that every
// tool the agent shells out to finds weave's stand-ins first.
func TestEnvPrefix(t *testing.T) {
	tests := []struct {
		name   string
		launch Launch
		want   string
	}{
		{
			name: "a launch naming nothing carries no assignments",
		},
		{
			name:   "the agent and its conversation",
			launch: Launch{AgentID: "a1", SessionID: "s1"},
			want:   "WEAVE_AGENT_ID='a1' WEAVE_SESSION_ID='s1' ",
		},
		{
			name:   "the tools directory goes ahead of the pane's own PATH",
			launch: Launch{ToolsDir: "/cache/weave-agent-tools", AgentID: "a1"},
			want:   `PATH='/cache/weave-agent-tools':"$PATH" WEAVE_AGENT_ID='a1' `,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, EnvPrefix(tt.launch))
		})
	}
}
