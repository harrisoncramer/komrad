package forge

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRollupOf covers the word a head's checks fold into, counting only the required checks when
// any is.
func TestRollupOf(t *testing.T) {
	t.Parallel()

	passed := StatusCheck{
		Name:       "test",
		State:      StatusCheckStateCompleted,
		Conclusion: "success",
	}
	failed := StatusCheck{
		Name:       "lint",
		State:      StatusCheckStateCompleted,
		Conclusion: "failure",
	}
	running := StatusCheck{Name: "e2e", State: StatusCheckStateRunning}

	tests := []struct {
		name   string
		checks []StatusCheck
		want   Checks
	}{
		{
			name: "no checks is none",
			want: ChecksNone,
		},
		{
			name: "every check passed is passing",
			checks: []StatusCheck{passed, {
				Name:       "docs",
				State:      StatusCheckStateCompleted,
				Conclusion: "skipped",
			}, {
				Name:       "scan",
				State:      StatusCheckStateCompleted,
				Conclusion: "neutral",
			}},
			want: ChecksPassing,
		},
		{
			name:   "a check finished without passing is failing",
			checks: []StatusCheck{passed, failed},
			want:   ChecksFailing,
		},
		{
			name: "a cancelled check is failing",
			checks: []StatusCheck{{
				Name:       "test",
				State:      StatusCheckStateCompleted,
				Conclusion: "cancelled",
			}},
			want: ChecksFailing,
		},
		{
			name:   "a failed check with another running is failing while running",
			checks: []StatusCheck{failed, running},
			want:   ChecksFailingRunning,
		},
		{
			name:   "a failed check with another queued is failing while running",
			checks: []StatusCheck{failed, {Name: "e2e", State: StatusCheckStateQueued}},
			want:   ChecksFailingRunning,
		},
		{
			name:   "a failed check with another waiting is failing",
			checks: []StatusCheck{failed, {Name: "deploy", State: StatusCheckStateWaiting}},
			want:   ChecksFailing,
		},
		{
			name:   "a check not finished with none failed is pending",
			checks: []StatusCheck{passed, running},
			want:   ChecksPending,
		},
		{
			name:   "a check waiting with none failed is pending",
			checks: []StatusCheck{passed, {Name: "deploy", State: StatusCheckStateWaiting}},
			want:   ChecksPending,
		},
		{
			name: "an optional check failing beside required ones passing is passing",
			checks: []StatusCheck{{
				Name:       "test",
				State:      StatusCheckStateCompleted,
				Conclusion: "success",
				Required:   true,
			}, failed},
			want: ChecksPassing,
		},
		{
			name: "an optional check running beside a required one failing is failing",
			checks: []StatusCheck{{
				Name:       "lint",
				State:      StatusCheckStateCompleted,
				Conclusion: "failure",
				Required:   true,
			}, running},
			want: ChecksFailing,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, RollupOf(tt.checks))
		})
	}
}

// TestDecidingStatusChecks covers which checks decide a head: the required ones when any is and every
// one otherwise.
func TestDecidingStatusChecks(t *testing.T) {
	t.Parallel()

	required := StatusCheck{Name: "test", Required: true}
	optional := StatusCheck{Name: "lint"}

	assert.Equal(t, []StatusCheck{required}, DecidingStatusChecks([]StatusCheck{optional, required}))
	assert.Equal(t, []StatusCheck{optional}, DecidingStatusChecks([]StatusCheck{optional}))
	assert.Empty(t, DecidingStatusChecks(nil))
}
