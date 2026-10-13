package forge

import (
	"testing"
	"time"

	"komrad/lib/xptr"

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
			name:   "a check komrad stopped reading beside one passed is passing",
			checks: []StatusCheck{passed, {Name: "e2e", State: StatusCheckStateUnknown}},
			want:   ChecksPassing,
		},
		{
			name:   "only checks komrad stopped reading is none",
			checks: []StatusCheck{{Name: "e2e", State: StatusCheckStateUnknown}},
			want:   ChecksNone,
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
		{
			name: "a failed check passing on a later run is passing",
			checks: []StatusCheck{{
				Name:       "lint",
				State:      StatusCheckStateCompleted,
				Conclusion: "failure",
			}, passed, {
				Name:       "lint",
				State:      StatusCheckStateCompleted,
				Conclusion: "success",
			}},
			want: ChecksPassing,
		},
		{
			name: "a passed check failing on a later run is failing",
			checks: []StatusCheck{passed, {
				Name:       "test",
				State:      StatusCheckStateCompleted,
				Conclusion: "failure",
			}},
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

	assert.Equal(t, []StatusCheck{required}, decidingStatusChecks([]StatusCheck{optional, required}))
	assert.Equal(t, []StatusCheck{optional}, decidingStatusChecks([]StatusCheck{optional}))
	assert.Empty(t, decidingStatusChecks(nil))
}

// TestFailedStatusChecks covers which checks count as failed: the deciding ones that finished without passing,
// each kind, source and name counted by its newest listing.
func TestFailedStatusChecks(t *testing.T) {
	t.Parallel()

	failed := StatusCheck{
		ProviderID: "1",
		Kind:       StatusCheckKindRun,
		Source:     "ci",
		Name:       "lint",
		State:      StatusCheckStateCompleted,
		Conclusion: "failure",
	}
	rerunPassed := StatusCheck{
		ProviderID: "2",
		Kind:       StatusCheckKindRun,
		Source:     "ci",
		Name:       "lint",
		State:      StatusCheckStateCompleted,
		Conclusion: "success",
	}
	otherWorkflow := StatusCheck{
		ProviderID: "3",
		Kind:       StatusCheckKindRun,
		Source:     "release",
		Name:       "lint",
		State:      StatusCheckStateCompleted,
		Conclusion: "failure",
	}
	requiredPassed := StatusCheck{
		ProviderID: "4",
		Kind:       StatusCheckKindRun,
		Source:     "ci",
		Name:       "test",
		State:      StatusCheckStateCompleted,
		Conclusion: "success",
		Required:   true,
	}
	running := StatusCheck{
		ProviderID: "5",
		Name:       "e2e",
		State:      StatusCheckStateRunning,
	}

	tests := []struct {
		name   string
		checks []StatusCheck
		want   []StatusCheck
	}{
		{
			name: "no checks is none failed",
			want: []StatusCheck{},
		},
		{
			name:   "a finished check that did not pass is failed",
			checks: []StatusCheck{failed, running},
			want:   []StatusCheck{failed},
		},
		{
			name:   "a rerun that passed hides the failure before it",
			checks: []StatusCheck{failed, rerunPassed},
			want:   []StatusCheck{},
		},
		{
			name:   "a rerun that failed counts in place of the pass before it",
			checks: []StatusCheck{rerunPassed, failed},
			want:   []StatusCheck{failed},
		},
		{
			name:   "a check of the same name from another source counts on its own",
			checks: []StatusCheck{failed, otherWorkflow, rerunPassed},
			want:   []StatusCheck{otherWorkflow},
		},
		{
			name:   "an optional check failing beside a required one is not failed",
			checks: []StatusCheck{failed, requiredPassed},
			want:   []StatusCheck{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, FailedStatusChecks(tt.checks))
		})
	}
}

// TestNewestRuns covers which run of each check stays listed, keyed by source, run event and name.
func TestNewestRuns(t *testing.T) {
	t.Parallel()

	at := func(hour int) *time.Time {
		return xptr.Ptr(time.Date(2026, 9, 20, hour, 0, 0, 0, time.UTC))
	}
	run := func(id, source, event, name string, runStartedAt *time.Time) StatusCheck {
		return StatusCheck{
			ProviderID:   id,
			Source:       source,
			RunEvent:     event,
			Name:         name,
			RunStartedAt: runStartedAt,
		}
	}
	older := run("1", "ci", "pull_request", "test", at(11))
	newer := run("2", "ci", "pull_request", "test", at(12))
	vet := run("3", "ci", "pull_request", "vet", at(11))
	otherSource := run("4", "scanner", "pull_request", "test", at(11))
	push := run("5", "ci", "push", "test", at(11))
	status := StatusCheck{ProviderID: "6", Name: "ci/legacy"}
	sameRunEarlier := run("7", "ci", "pull_request", "lint", at(11))
	sameRunEarlier.StartedAt = at(11)
	sameRunLater := run("8", "ci", "pull_request", "lint", at(11))
	sameRunLater.StartedAt = at(12)

	tests := []struct {
		name   string
		checks []StatusCheck
		want   []StatusCheck
	}{
		{
			name: "no checks is none",
		},
		{
			name:   "a rerun hides the run before it",
			checks: []StatusCheck{older, vet, newer},
			want:   []StatusCheck{vet, newer},
		},
		{
			name:   "the newest run stays when it is listed first",
			checks: []StatusCheck{newer, older},
			want:   []StatusCheck{newer},
		},
		{
			name:   "a check of the same name from another source counts on its own",
			checks: []StatusCheck{older, otherSource},
			want:   []StatusCheck{older, otherSource},
		},
		{
			name:   "runs of one workflow started by different events each keep their checks",
			checks: []StatusCheck{push, older},
			want:   []StatusCheck{push, older},
		},
		{
			name:   "a check that ran in no run stays",
			checks: []StatusCheck{status, older},
			want:   []StatusCheck{status, older},
		},
		{
			name:   "within one run the check that started later stays",
			checks: []StatusCheck{sameRunLater, sameRunEarlier},
			want:   []StatusCheck{sameRunLater},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, NewestRuns(tt.checks))
		})
	}
}
