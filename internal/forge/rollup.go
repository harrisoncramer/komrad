package forge

import (
	"slices"

	"komrad/lib/xslice"
)

// passingConclusions are the conclusions a completed check counts as passed with.
var passingConclusions = []string{"success", "neutral", "skipped"}

// RollupOf is where the checks on a head stand, counting only the required ones when any is.
func RollupOf(checks []StatusCheck) Checks {
	counted := slices.DeleteFunc(decidingStatusChecks(checks), func(check StatusCheck) bool {
		return check.State == StatusCheckStateUnknown
	})

	failing := slices.ContainsFunc(counted, StatusCheck.IsFailing)
	unfinished := slices.ContainsFunc(counted, func(check StatusCheck) bool { return check.State != StatusCheckStateCompleted })
	switch {
	case failing && slices.ContainsFunc(counted, StatusCheck.IsRunning):
		return ChecksFailingRunning
	case failing:
		return ChecksFailing
	case unfinished:
		return ChecksPending
	case len(counted) > 0:
		return ChecksPassing
	}

	return ChecksNone
}

// decidingStatusChecks is the checks that decide where a head stands, the required ones when any is and every one otherwise.
func decidingStatusChecks(checks []StatusCheck) []StatusCheck {
	if !slices.ContainsFunc(checks, func(check StatusCheck) bool { return check.Required }) {
		return slices.Clone(checks)
	}

	return slices.DeleteFunc(slices.Clone(checks), func(check StatusCheck) bool { return !check.Required })
}

// IsFailing says whether a check finished without passing.
func (c StatusCheck) IsFailing() bool {
	return c.State == StatusCheckStateCompleted && !slices.Contains(passingConclusions, c.Conclusion)
}

// IsRunning says whether a check is queued or running.
func (c StatusCheck) IsRunning() bool {
	return c.State == StatusCheckStateQueued || c.State == StatusCheckStateRunning
}

// IsInFlight says whether a check or any of its steps is queued, running or waiting.
func (c StatusCheck) IsInFlight() bool {
	return isInFlight(c.State) || slices.ContainsFunc(c.Steps, func(step StatusCheckStep) bool { return isInFlight(step.State) })
}

// AnyInFlight says whether any of the checks is in flight.
func AnyInFlight(checks []StatusCheck) bool {
	return slices.ContainsFunc(checks, StatusCheck.IsInFlight)
}

// isInFlight says whether a check or step in the state has yet to finish.
func isInFlight(state StatusCheckState) bool {
	return state == StatusCheckStateQueued || state == StatusCheckStateRunning || state == StatusCheckStateWaiting
}

// FailedStatusChecks is the checks that decide a head and failed, counting only the newest of the checks
// sharing a kind, source and name, given checks listed oldest first.
func FailedStatusChecks(checks []StatusCheck) []StatusCheck {
	newest := xslice.LastBy(decidingStatusChecks(checks), func(check StatusCheck) statusCheckKey {
		return statusCheckKey{
			kind:   check.Kind,
			source: check.Source,
			name:   check.Name,
		}
	})

	return slices.DeleteFunc(newest, func(check StatusCheck) bool { return !check.IsFailing() })
}

type statusCheckKey struct {
	kind   StatusCheckKind
	source string
	name   string
}
