package internaljobs

import (
	"context"
	"errors"
	"testing"

	"komrad/internal/agenterr"
	"komrad/internal/agents"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAgentArchiver struct {
	err      error
	archived [][]string
}

func (f *fakeAgentArchiver) ArchiveAgents(_ context.Context, params agents.ArchiveAgentsParams) error {
	f.archived = append(f.archived, params.IDs)

	return f.err
}

// TestArchiveAgentWorker pins which archive failures the job retries.
func TestArchiveAgentWorker(t *testing.T) {
	failed := errors.New("tmux is gone")

	tests := []struct {
		name       string
		archiveErr error
		wantErr    error
	}{
		{name: "an agent archived"},
		{name: "a deleted agent", archiveErr: agenterr.ErrAgentNotFound},
		{name: "a failure that might pass", archiveErr: failed, wantErr: failed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			archiver := &fakeAgentArchiver{err: tt.archiveErr}

			err := NewArchiveAgentWorker(archiver).Work(t.Context(), &river.Job[ArchiveAgentArgs]{Args: ArchiveAgentArgs{AgentID: "a"}})

			require.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, [][]string{{"a"}}, archiver.archived)
		})
	}
}
