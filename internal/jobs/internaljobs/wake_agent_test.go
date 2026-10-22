package internaljobs

import (
	"context"
	"errors"
	"testing"

	"komrad/internal/agenterr"
	"komrad/internal/agents"
	agentstore "komrad/internal/store/agent"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAgentResumer struct {
	err     error
	resumed []uuid.UUID
}

func (f *fakeAgentResumer) ResumeAgent(_ context.Context, params agents.ResumeAgentParams) (*agentstore.Agent, error) {
	f.resumed = append(f.resumed, params.ID)
	if f.err != nil {
		return nil, f.err
	}

	return &agentstore.Agent{ID: params.ID}, nil
}

// TestWakeAgentWorker pins which failures to resume an agent are left for River to retry.
func TestWakeAgentWorker(t *testing.T) {
	failed := errors.New("tmux is gone")

	tests := []struct {
		name      string
		resumeErr error
		wantErr   error
	}{
		{name: "an agent put back in a pane"},
		{name: "an agent already being resumed", resumeErr: agenterr.ErrResumeInFlight},
		{name: "an archived agent", resumeErr: agenterr.ErrAgentArchived},
		{name: "a deleted agent", resumeErr: agenterr.ErrAgentNotFound},
		{
			name:      "a failure that might pass",
			resumeErr: failed,
			wantErr:   failed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resumer := &fakeAgentResumer{err: tt.resumeErr}
			agentID := uuid.New()

			err := NewWakeAgentWorker(resumer).Work(t.Context(), &river.Job[WakeAgentArgs]{Args: WakeAgentArgs{AgentID: agentID}})

			require.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, []uuid.UUID{agentID}, resumer.resumed)
		})
	}
}
