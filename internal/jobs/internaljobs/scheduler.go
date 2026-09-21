package internaljobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"weave/internal/schedules"

	"github.com/riverqueue/river"
)

type periodicJobRegistrar interface {
	Add(id string, cron river.PeriodicSchedule) error
	Remove(id string)
}

type Scheduler struct {
	schedules  ScheduleLister
	periodic   periodicJobRegistrar
	registered map[string]registration
}

type registration struct {
	cron     string
	timezone string
}

// NewScheduler builds the reconciler over the schedules it reads and the periodic jobs it
// maintains.
func NewScheduler(schedules ScheduleLister, periodic periodicJobRegistrar) *Scheduler {
	return &Scheduler{
		schedules:  schedules,
		periodic:   periodic,
		registered: make(map[string]registration),
	}
}

// Reconcile makes River's periodic jobs match the schedules table.
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

		// Returning on a bad row would leave the schedules after it unregistered and skip
		// the removal below that takes a paused schedule off.
		cron, err := schedules.ParseCron(schedule.Cron, schedule.Timezone)
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
