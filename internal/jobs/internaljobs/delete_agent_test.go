package internaljobs

import (
	"context"
	"errors"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAgentDeleter struct {
	deleteErr error
	deleted   []string
	failed    []string
}

func (f *fakeAgentDeleter) DeleteAgent(_ context.Context, id string) error {
	f.deleted = append(f.deleted, id)

	return f.deleteErr
}

func (f *fakeAgentDeleter) FailDelete(_ context.Context, id string, _ error) error {
	f.failed = append(f.failed, id)

	return nil
}

// TestDeleteAgentWorker pins a failed delete being retried until its last attempt, which
// fails the agent's cleanup rather than leaving it claimed.
func TestDeleteAgentWorker(t *testing.T) {
	tests := []struct {
		name       string
		deleteErr  error
		attempt    int
		wantErr    bool
		wantFailed []string
	}{
		{
			name:    "a delete that works is done",
			attempt: 1,
		},
		{
			name:      "a delete that fails early is retried",
			deleteErr: errors.New("the store is away"),
			attempt:   1,
			wantErr:   true,
		},
		{
			name:       "a delete that fails on its last attempt fails the cleanup",
			deleteErr:  errors.New("the store is away"),
			attempt:    3,
			wantErr:    true,
			wantFailed: []string{"a1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeAgentDeleter{deleteErr: tt.deleteErr}
			worker := NewDeleteAgentWorker(fake)

			err := worker.Work(t.Context(), &river.Job[DeleteAgentArgs]{
				JobRow: &rivertype.JobRow{Attempt: tt.attempt, MaxAttempts: 3},
				Args:   DeleteAgentArgs{AgentID: "a1"},
			})

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, []string{"a1"}, fake.deleted)
			assert.Equal(t, tt.wantFailed, fake.failed)
		})
	}
}
