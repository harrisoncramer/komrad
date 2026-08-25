package jobs

import (
	"fmt"
	"time"

	"weave/internal/jobs/internaljobs"

	"github.com/riverqueue/river"
)

type RiverPeriodic struct {
	bundle *river.PeriodicJobBundle
}

// NewRiverPeriodic wraps a client's periodic jobs.
func NewRiverPeriodic(bundle *river.PeriodicJobBundle) *RiverPeriodic {
	return &RiverPeriodic{bundle: bundle}
}

// Add registers a periodic job on cron under an id it can later be removed by.
func (r *RiverPeriodic) Add(id string, cron river.PeriodicSchedule) error {
	if _, err := r.bundle.AddSafely(periodicJobFor(id, cron)); err != nil {
		return fmt.Errorf("failed to add a periodic job for %q: %w", id, err)
	}

	return nil
}

// Remove takes a registration back off, and is a no-op when nothing holds that id.
func (r *RiverPeriodic) Remove(id string) {
	r.bundle.RemoveByID(id)
}

func periodicJobFor(scheduleID string, cron river.PeriodicSchedule) *river.PeriodicJob {
	return river.NewPeriodicJob(
		cron,
		func() (river.JobArgs, *river.InsertOpts) {
			return internaljobs.LaunchArgsFor(scheduleID, cron, time.Now())
		},
		&river.PeriodicJobOpts{ID: scheduleID},
	)
}
