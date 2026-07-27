package cli

import (
	"testing"

	agentv1 "claude-agents/proto/gen/agent/v1"

	agentstore "claude-agents/internal/store/agent"

	"github.com/stretchr/testify/assert"
)

// listing builds the agents the summary renders from. It is what the service
// would return, so the test needs no database of its own.
func listing(states ...string) []*agentv1.Agent {
	out := make([]*agentv1.Agent, 0, len(states))
	for i, state := range states {
		out = append(out, &agentv1.Agent{Name: string(rune('a' + i)), State: state})
	}

	return out
}

func TestRenderStatus(t *testing.T) {
	tests := []struct {
		name         string
		states       []string
		wantEmpty    bool
		wantContains []string
	}{
		{
			name:      "no agents renders nothing",
			states:    nil,
			wantEmpty: true,
		},
		{
			name:      "only new agents render nothing",
			states:    []string{agentstore.StateNew, agentstore.StateNew},
			wantEmpty: true,
		},
		{
			name:         "done agents alone render a done segment",
			states:       []string{agentstore.StateDone},
			wantContains: []string{"1 done"},
		},
		{
			name:         "a mix renders every non-empty segment",
			states:       []string{agentstore.StateWaiting, agentstore.StateThinking, agentstore.StateThinking, agentstore.StateDone, agentstore.StateNew},
			wantContains: []string{"1 waiting", "2 working", "1 done"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderStatus(listing(tt.states...))
			if tt.wantEmpty {
				assert.Empty(t, got)
				return
			}
			for _, want := range tt.wantContains {
				assert.Contains(t, got, want)
			}
		})
	}
}
