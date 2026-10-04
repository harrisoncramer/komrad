package forge

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRollupOf covers the word a head's checks fold into, counting only the required checks when
// any is.
func TestRollupOf(t *testing.T) {
	t.Parallel()

	passed := Check{
		Name:       "test",
		State:      CheckStateCompleted,
		Conclusion: "success",
	}
	failed := Check{
		Name:       "lint",
		State:      CheckStateCompleted,
		Conclusion: "failure",
	}
	running := Check{Name: "e2e", State: CheckStateRunning}

	tests := []struct {
		name   string
		checks []Check
		want   Checks
	}{
		{
			name: "no checks is none",
			want: ChecksNone,
		},
		{
			name: "every check passed is passing",
			checks: []Check{passed, {
				Name:       "docs",
				State:      CheckStateCompleted,
				Conclusion: "skipped",
			}, {
				Name:       "scan",
				State:      CheckStateCompleted,
				Conclusion: "neutral",
			}},
			want: ChecksPassing,
		},
		{
			name:   "a check finished without passing is failing",
			checks: []Check{passed, failed},
			want:   ChecksFailing,
		},
		{
			name: "a cancelled check is failing",
			checks: []Check{{
				Name:       "test",
				State:      CheckStateCompleted,
				Conclusion: "cancelled",
			}},
			want: ChecksFailing,
		},
		{
			name:   "a failed check with another running is failing while running",
			checks: []Check{failed, running},
			want:   ChecksFailingRunning,
		},
		{
			name:   "a failed check with another queued is failing while running",
			checks: []Check{failed, {Name: "e2e", State: CheckStateQueued}},
			want:   ChecksFailingRunning,
		},
		{
			name:   "a failed check with another waiting is failing",
			checks: []Check{failed, {Name: "deploy", State: CheckStateWaiting}},
			want:   ChecksFailing,
		},
		{
			name:   "a check not finished with none failed is pending",
			checks: []Check{passed, running},
			want:   ChecksPending,
		},
		{
			name:   "a check waiting with none failed is pending",
			checks: []Check{passed, {Name: "deploy", State: CheckStateWaiting}},
			want:   ChecksPending,
		},
		{
			name: "an optional check failing beside required ones passing is passing",
			checks: []Check{{
				Name:       "test",
				State:      CheckStateCompleted,
				Conclusion: "success",
				Required:   true,
			}, failed},
			want: ChecksPassing,
		},
		{
			name: "an optional check running beside a required one failing is failing",
			checks: []Check{{
				Name:       "lint",
				State:      CheckStateCompleted,
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

// TestDecidingChecks covers which checks decide a head: the required ones when any is and every
// one otherwise.
func TestDecidingChecks(t *testing.T) {
	t.Parallel()

	required := Check{Name: "test", Required: true}
	optional := Check{Name: "lint"}

	assert.Equal(t, []Check{required}, DecidingChecks([]Check{optional, required}))
	assert.Equal(t, []Check{optional}, DecidingChecks([]Check{optional}))
	assert.Empty(t, DecidingChecks(nil))
}
