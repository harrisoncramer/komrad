package internaljobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeOldCheckRecoverer struct {
	outcome   RecoverOutcome
	err       error
	recovered []RecoverOldChecksArgs
}

func (f *fakeOldCheckRecoverer) RecoverOldChecks(_ context.Context, args RecoverOldChecksArgs) (RecoverOutcome, error) {
	f.recovered = append(f.recovered, args)

	return f.outcome, f.err
}

// TestRecoverOldChecksWorker pins how what came of a read turns into River's next move.
func TestRecoverOldChecksWorker(t *testing.T) {
	failed := errors.New("the store is gone")

	tests := []struct {
		name       string
		outcome    RecoverOutcome
		recoverErr error
		wantErr    error
		wantSnooze time.Duration
	}{
		{name: "a step done"},
		{
			name:       "a forge that cannot be asked yet",
			outcome:    RecoverOutcome{Snooze: 5 * time.Minute},
			wantSnooze: 5 * time.Minute,
		},
		{
			name:       "a failure that might pass",
			recoverErr: failed,
			wantErr:    failed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recoverer := &fakeOldCheckRecoverer{outcome: tt.outcome, err: tt.recoverErr}
			args := RecoverOldChecksArgs{PullRequestID: "p", HeadSHA: "a", Step: 1}

			err := NewRecoverOldChecksWorker(recoverer).Work(t.Context(), &river.Job[RecoverOldChecksArgs]{Args: args})

			switch {
			case tt.wantSnooze > 0:
				var snoozed *river.JobSnoozeError
				require.ErrorAs(t, err, &snoozed)
				assert.Equal(t, tt.wantSnooze, snoozed.Duration)
			default:
				require.ErrorIs(t, err, tt.wantErr)
			}
			assert.Equal(t, []RecoverOldChecksArgs{args}, recoverer.recovered)
		})
	}
}
