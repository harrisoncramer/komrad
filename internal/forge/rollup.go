package forge

import (
	"slices"
	"strings"
	"time"

	"komrad/lib/xptr"
	"komrad/lib/xslice"
)

// passingConclusions are the conclusions a completed check counts as passed with.
var passingConclusions = []string{"success", "neutral", "skipped"}

// RollupOf is where the checks on a head stand, counting the newest run of each and only the required ones when any is.
func RollupOf(checks []StatusCheck) Checks {
	counted := slices.DeleteFunc(newestStatusChecks(decidingStatusChecks(checks)), func(check StatusCheck) bool {
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
	return c.State == StatusCheckStateCompleted && !IsPassingConclusion(c.Conclusion)
}

// IsPassing says whether a check completed with a conclusion that counts as passed.
func (c StatusCheck) IsPassing() bool {
	return c.State == StatusCheckStateCompleted && IsPassingConclusion(c.Conclusion)
}

// IsPassing says whether a step completed with a conclusion that counts as passed.
func (s StatusCheckStep) IsPassing() bool {
	return s.State == StatusCheckStateCompleted && IsPassingConclusion(s.Conclusion)
}

// IsPassingConclusion says whether a completed check or step with the conclusion counts as passed.
func IsPassingConclusion(conclusion string) bool {
	return slices.Contains(passingConclusions, conclusion)
}

// IsRunning says whether a check is queued or running.
func (c StatusCheck) IsRunning() bool {
	return c.State == StatusCheckStateQueued || c.State == StatusCheckStateRunning
}

// FailedStatusChecks is the checks that decide a head and failed, counting only the newest of the checks
// sharing a kind, source and name, given checks listed oldest first.
func FailedStatusChecks(checks []StatusCheck) []StatusCheck {
	return slices.DeleteFunc(newestStatusChecks(decidingStatusChecks(checks)), func(check StatusCheck) bool { return !check.IsFailing() })
}

type statusCheckKey struct {
	kind   StatusCheckKind
	source string
	name   string
}

// newestStatusChecks is the newest of the checks sharing a kind, source and name, given checks listed oldest first.
func newestStatusChecks(checks []StatusCheck) []StatusCheck {
	return xslice.LastBy(checks, func(check StatusCheck) statusCheckKey {
		return statusCheckKey{
			kind:   check.Kind,
			source: check.Source,
			name:   check.Name,
		}
	})
}

type runKey struct {
	source string
	event  string
	name   string
}

// NewestRuns is the newest run of each check sharing a source, run event and name, in the order listed.
func NewestRuns(checks []StatusCheck) []StatusCheck {
	newest := map[runKey]StatusCheck{}
	for _, check := range checks {
		if held, ok := newest[check.runKey()]; !ok || compareRuns(held, check) < 0 {
			newest[check.runKey()] = check
		}
	}

	return slices.DeleteFunc(slices.Clone(checks), func(check StatusCheck) bool {
		return newest[check.runKey()].ProviderID != check.ProviderID
	})
}

func (c StatusCheck) runKey() runKey {
	return runKey{
		source: c.Source,
		event:  c.RunEvent,
		name:   c.Name,
	}
}

func compareRuns(a, b StatusCheck) int {
	if order := compareTimes(a.RunStartedAt, b.RunStartedAt); order != 0 {
		return order
	}
	if order := compareTimes(a.StartedAt, b.StartedAt); order != 0 {
		return order
	}

	return strings.Compare(a.ProviderID, b.ProviderID)
}

func compareTimes(a, b *time.Time) int {
	return xptr.Deref(a).Compare(xptr.Deref(b))
}
