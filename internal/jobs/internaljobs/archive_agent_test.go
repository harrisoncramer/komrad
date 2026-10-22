package internaljobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"komrad/internal/agenterr"
	"komrad/internal/agents"
	agentstore "komrad/internal/store/agent"
	"komrad/internal/storetest"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAgentArchiver struct {
	err      error
	archived [][]uuid.UUID
}

func (f *fakeAgentArchiver) ArchiveAgents(_ context.Context, params agents.ArchiveAgentsParams) error {
	f.archived = append(f.archived, params.IDs)

	return f.err
}

// TestArchiveAgentWorker pins when the job archives, waits on the reviewer's turn, or retries.
func TestArchiveAgentWorker(t *testing.T) {
	failed := errors.New("tmux is gone")

	tests := []struct {
		name         string
		state        agentstore.State
		age          time.Duration
		deleted      bool
		archiveErr   error
		wantErr      error
		wantSnooze   bool
		wantArchived bool
	}{
		{name: "a deleted agent", deleted: true},
		{
			name:         "a failure that might pass",
			state:        agentstore.StateIdle,
			archiveErr:   failed,
			wantErr:      failed,
			wantArchived: true,
		},
		{
			name:         "an agent deleted as it is archived",
			state:        agentstore.StateIdle,
			archiveErr:   agenterr.ErrAgentNotFound,
			wantArchived: true,
		},
		{
			name:       "a waiting agent waited on",
			state:      agentstore.StateWaiting,
			wantSnooze: true,
		},
		{
			name:       "a thinking agent waited on",
			state:      agentstore.StateThinking,
			wantSnooze: true,
		},
		{
			name:         "a thinking agent archived past the limit",
			state:        agentstore.StateThinking,
			age:          archiveLimit,
			wantArchived: true,
		},
		{
			name:         "an idle agent archived",
			state:        agentstore.StateIdle,
			wantArchived: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stores := storetest.Open(t)
			id := uuid.New()
			if !tt.deleted {
				id = stores.SeedAgent(t, agentstore.Agent{
					ID:    id,
					Name:  "reviewer",
					Cwd:   "/repo",
					Role:  agentstore.RoleReviewer,
					State: tt.state,
				}).ID
			}
			archiver := &fakeAgentArchiver{err: tt.archiveErr}
			worker := NewArchiveAgentWorker(NewArchiveAgentWorkerParams{Archiver: archiver, Agents: stores.Agents})

			err := worker.Work(t.Context(), &river.Job[ArchiveAgentArgs]{
				JobRow: &rivertype.JobRow{CreatedAt: time.Now().Add(-tt.age)},
				Args:   ArchiveAgentArgs{AgentID: id},
			})

			if tt.wantSnooze {
				var snooze *river.JobSnoozeError
				require.ErrorAs(t, err, &snooze)
			} else {
				require.ErrorIs(t, err, tt.wantErr)
			}
			if tt.wantArchived {
				assert.Equal(t, [][]uuid.UUID{{id}}, archiver.archived)
			} else {
				assert.Empty(t, archiver.archived)
			}
		})
	}
}
