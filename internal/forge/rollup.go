package forge

import "slices"

// passingConclusions are the conclusions a completed check counts as passed with.
var passingConclusions = []string{"success", "neutral", "skipped"}

// RollupOf is where the checks on a head stand, counting only the required ones when any is.
func RollupOf(checks []StatusCheck) Checks {
	counted := DecidingStatusChecks(checks)

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

// DecidingStatusChecks is the checks that decide where a head stands, the required ones when any is and every one otherwise.
func DecidingStatusChecks(checks []StatusCheck) []StatusCheck {
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
