package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"weave/internal/agents"

	"github.com/riverqueue/river"
)

// periodicJobRegistrar is what the reconciler needs to keep River's registrations in step, stated
// in schedule ids rather than River's handles so a test can assert on what was armed. The
// job is built by the implementation, since the id a registration is filed under has to be
// the id it is later removed by.
type periodicJobRegistrar interface {
	Add(id string, cron river.PeriodicSchedule) error
	Remove(id string)
}

// RiverPeriodic adapts a River client's periodic job bundle to what the scheduler drives.
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

// Scheduler keeps River's periodic jobs matching the schedules table. River holds its
// periodic jobs in memory while the schedules live in Postgres, so every pass reads the
// table and makes the registered set match, and a schedule created through the panel, the
// CLI, or an MCP tool is picked up without any of them knowing this exists.
type Scheduler struct {
	schedules ScheduleReader
	periodic  periodicJobRegistrar
	// registered is what this has told River about, keyed by schedule id, valued by what
	// the job was built from so a pass can tell a changed schedule from a renamed one.
	registered map[string]registration
}

// registration is the part of a schedule the periodic job is built from. A change to any of
// it means the registered job no longer matches the row.
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

		// An expression that changed has to replace its registration rather than sit
		// beside it, or the schedule would fire on both. The entry goes with it, so this
		// never claims a job River no longer holds.
		if registered {
			s.periodic.Remove(schedule.ID)
			delete(s.registered, schedule.ID)
		}

		// A refused registration is left unrecorded so the next pass tries it again
		// rather than treating it as done.
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

// periodicJobFor builds the River periodic job for one schedule, filed under the id it
// will be removed by. The occurrence comes from the schedule rather than the clock, so two
// inserts standing for the same firing carry identical arguments and River's unique index
// collapses them into one run.
func periodicJobFor(scheduleID string, cron river.PeriodicSchedule) *river.PeriodicJob {
	return river.NewPeriodicJob(
		cron,
		func() (river.JobArgs, *river.InsertOpts) {
			return launchArgsFor(scheduleID, cron, time.Now())
		},
		&river.PeriodicJobOpts{ID: scheduleID},
	)
}

// launchArgsFor builds the arguments for one firing. Nil arguments tell River there is
// nothing to insert, which is the answer for a schedule with no occurrence to name. Filing
// the job under the zero time instead would stand for a firing that never happened, and
// since the occurrence is what the unique index is built from, every later one would
// collapse into it.
func launchArgsFor(scheduleID string, cron river.PeriodicSchedule, now time.Time) (river.JobArgs, *river.InsertOpts) {
	occurrence := occurrenceFor(cron, now)
	if occurrence.IsZero() {
		return nil, nil
	}

	return LaunchAgentArgs{ScheduleID: scheduleID, ScheduledFor: occurrence}, nil
}

// staleness is how late a firing may be and still be worth running. River works out when a
// schedule is next due and inserts then, so an ordinary firing is only as late as the
// enqueuer itself. What pushes a job well past its firing is a machine asleep through it,
// and a run that stale is one nobody was around for.
const staleness = time.Hour

// skew is how far ahead of an occurrence the construction may run and still be counted
// as standing for it, covering a firing that lands a hair before the boundary.
const skew = 5 * time.Second

// occurrenceFor names the firing a job stands for, which is the schedule's last occurrence at
// or before now. It has to come from the schedule, since the clock alone cannot tell a job
// built late for one occurrence from one built early for the next, and naming the wrong one
// either collides with the previous firing on the unique index and vanishes, or blocks the
// firing that follows.
//
// It walks forward rather than back because a cron schedule can only say what comes next, so
// reaching an occurrence means starting behind it. Starting a whole staleness behind is what
// makes a late firing reachable at all, and the cutoff is what stops the walk running past
// now onto a firing still to come.
//
// The zero time means there is nothing to name, and queues nothing. That covers a schedule
// with no occurrence left, and one whose last firing is staler than this will run.
func occurrenceFor(cron river.PeriodicSchedule, now time.Time) time.Time {
	cutoff := now.Add(skew)

	occurrence := cron.Next(now.Add(-staleness))
	if occurrence.IsZero() || occurrence.After(cutoff) {
		return time.Time{}
	}

	for {
		next := cron.Next(occurrence)
		if next.IsZero() || next.After(cutoff) {
			return occurrence
		}

		occurrence = next
	}
}
