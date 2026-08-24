package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"weave/internal/agents"

	"github.com/riverqueue/river"
)

type periodicJobRegistrar interface {
	Add(id string, cron river.PeriodicSchedule) error
	Remove(id string)
}

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

type Scheduler struct {
	schedules ScheduleReader
	periodic  periodicJobRegistrar

	registered map[string]registration
}

type registration struct {
	cron     string
	timezone string
}

// NewScheduler builds the reconciler over the schedules it reads and the periodic jobs it
// maintains.
func NewScheduler(schedules ScheduleReader, periodic periodicJobRegistrar) *Scheduler {
	return &Scheduler{
		schedules:  schedules,
		periodic:   periodic,
		registered: make(map[string]registration),
	}
}

// Reconcile makes River's periodic jobs match the schedules table. Enabled schedules are
// registered, paused and deleted ones are removed, and a schedule whose expression changed
// is re-registered against the new one.
//
// A row that cannot be parsed or that River refuses is gathered and the pass carries on,
// since returning there would leave the schedules after it unregistered and skip the
// removal below, which is what takes a paused schedule off.
func (s *Scheduler) Reconcile(ctx context.Context) error {
	listed, err := s.schedules.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list schedules to reconcile: %w", err)
	}

	var failures []error

	live := make(map[string]bool, len(listed))
	for _, schedule := range listed {
		if !schedule.Enabled {
			continue
		}

		cron, err := agents.ParseCron(schedule.Cron, schedule.Timezone)
		if err != nil {
			continue
		}

		live[schedule.ID] = true

		want := registration{cron: schedule.Cron, timezone: schedule.Timezone}
		have, registered := s.registered[schedule.ID]
		if registered && have == want {
			continue
		}

		if registered {
			s.periodic.Remove(schedule.ID)
			delete(s.registered, schedule.ID)
		}

		if err := s.periodic.Add(schedule.ID, cron); err != nil {
			failures = append(failures, fmt.Errorf("failed to register schedule %q: %w", schedule.ID, err))
			continue
		}

		s.registered[schedule.ID] = want
	}

	for id := range s.registered {
		if live[id] {
			continue
		}

		s.periodic.Remove(id)
		delete(s.registered, id)
	}

	return errors.Join(failures...)
}

func periodicJobFor(scheduleID string, cron river.PeriodicSchedule) *river.PeriodicJob {
	return river.NewPeriodicJob(
		cron,
		func() (river.JobArgs, *river.InsertOpts) {
			return launchArgsFor(scheduleID, cron, time.Now())
		},
		&river.PeriodicJobOpts{ID: scheduleID},
	)
}

func launchArgsFor(scheduleID string, cron river.PeriodicSchedule, now time.Time) (river.JobArgs, *river.InsertOpts) {
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
