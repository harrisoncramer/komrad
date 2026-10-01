package internaljobs

import (
	"time"

	"github.com/riverqueue/river"
)

// LaunchArgsFor builds the arguments for one firing of a schedule, or nothing at all when
// the expression has no occurrence to launch.
func LaunchArgsFor(scheduleID string, cron river.PeriodicSchedule, now time.Time) (river.JobArgs, *river.InsertOpts) {
	occurrence := occurrenceFor(cron, now)
	if occurrence.IsZero() {
		return nil, nil
	}

	return LaunchAgentArgs{ScheduleID: scheduleID, ScheduledFor: occurrence}, nil
}

const lateness = time.Hour

const skew = 5 * time.Second

func occurrenceFor(cron river.PeriodicSchedule, now time.Time) time.Time {
	cutoff := now.Add(skew)

	occurrence := cron.Next(now.Add(-lateness))
	if occurrence.IsZero() {
		return occurrence
	}

	for {
		next := cron.Next(occurrence)
		if next.IsZero() || next.After(cutoff) {
			return occurrence
		}

		occurrence = next
	}
}
