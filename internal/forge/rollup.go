package forge

import "slices"

// passingConclusions are the conclusions a completed check counts as passed with.
var passingConclusions = []string{"success", "neutral", "skipped"}

// RollupOf is where the checks on a head stand, counting only the required ones when any is.
func RollupOf(checks []Check) Checks {
	counted := DecidingChecks(checks)

	failing := slices.ContainsFunc(counted, Check.IsFailing)
	unfinished := slices.ContainsFunc(counted, func(check Check) bool { return check.State != CheckStateCompleted })
	switch {
	case failing && slices.ContainsFunc(counted, Check.IsRunning):
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

// DecidingChecks is the checks that decide where a head stands, the required ones when any is and every one otherwise.
func DecidingChecks(checks []Check) []Check {
	if !slices.ContainsFunc(checks, func(check Check) bool { return check.Required }) {
		return checks
	}

	return slices.DeleteFunc(slices.Clone(checks), func(check Check) bool { return !check.Required })
}

// IsFailing says whether a check finished without passing.
func (c Check) IsFailing() bool {
	return c.State == CheckStateCompleted && !slices.Contains(passingConclusions, c.Conclusion)
}

// IsRunning says whether a check is queued or running.
func (c Check) IsRunning() bool {
	return c.State == CheckStateQueued || c.State == CheckStateRunning
}
