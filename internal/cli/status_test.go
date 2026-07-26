package cli

import (
	"testing"

	sessionv1 "claude-agents/proto/gen/session/v1"

	"claude-agents/internal/store/session"

	"github.com/stretchr/testify/assert"
)

// sessions builds the listing the summary renders from. It is what the service
// would return, so the test needs no database of its own.
func sessions(states ...string) []*sessionv1.Session {
	out := make([]*sessionv1.Session, 0, len(states))
	for i, state := range states {
		out = append(out, &sessionv1.Session{Name: string(rune('a' + i)), State: state})
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
			states:    []string{session.StateNew, session.StateNew},
			wantEmpty: true,
		},
		{
			name:         "done agents alone render a done segment",
			states:       []string{session.StateDone},
			wantContains: []string{"1 done"},
		},
		{
			name:         "a mix renders every non-empty segment",
			states:       []string{session.StateWaiting, session.StateThinking, session.StateThinking, session.StateDone, session.StateNew},
			wantContains: []string{"1 waiting", "2 working", "1 done"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderStatus(sessions(tt.states...))
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
