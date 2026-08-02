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
		func() (river.JobArgs, *river.InsertOpts) { return launchArgsFor(scheduleID, cron, time.Now()) },
		&river.PeriodicJobOpts{ID: scheduleID},
	)
}

// launchArgsFor is what River's enqueuer builds each time the schedule comes round. A
// schedule with no occurrence to name queues nothing, which River takes nil arguments to
// mean, since a firing filed under the zero time stands for none and every later one would
// dedup into the first.
func launchArgsFor(scheduleID string, cron river.PeriodicSchedule, now time.Time) (river.JobArgs, *river.InsertOpts) {
	occurrence := occurrenceFor(cron, now)
	if occurrence.IsZero() {
		return nil, nil
	}

	return LaunchAgentArgs{ScheduleID: scheduleID, ScheduledFor: occurrence}, nil
}

// firstWindow is how far back the search starts looking. It is not a bound on how late the
// job may be built, only where the walk begins, since the search doubles the window back
// when it holds nothing. An hour keeps a minutely schedule to sixty steps.
const firstWindow = time.Hour

// horizon is as far back as the search reaches. The cron library looks five years forward
// from wherever it is asked, so nothing further back than this can be found however wide the
// window grows.
const horizon = 5 * 365 * 24 * time.Hour

// skew is how far ahead of an occurrence the construction may run and still be counted
// as standing for it, covering a firing that lands a hair before the boundary.
const skew = 5 * time.Second

// occurrenceFor names the firing a job stands for: the schedule's last occurrence at or
// before now. It has to come from the schedule, since the clock alone cannot tell a job
// built late for one occurrence from one built early for the next, and naming the wrong one
// either collides with the previous firing on the unique index and vanishes, or blocks the
// firing that follows.
//
// It walks forward rather than back because a cron schedule can only say what comes next, so
// reaching an occurrence means starting behind it. How far behind depends on the schedule and
// not on the clock. A yearly firing's last occurrence is months back whether the job was built
// promptly or not, so the search doubles its window back until one window holds an occurrence
// rather than assuming a fixed distance covers it.
//
// A schedule with no occurrence to reach reports the zero time, which covers both a date the
// calendar does not have and a last firing further back than the library will look.
func occurrenceFor(cron river.PeriodicSchedule, now time.Time) time.Time {
	cutoff := now.Add(skew)

	for window := firstWindow; ; window = min(window*2, horizon) {
		if occurrence := findLastOccurrenceBetween(cron, now.Add(-window), cutoff); !occurrence.IsZero() {
			return occurrence
		}

		if window == horizon {
			return time.Time{}
		}
	}
}

// findLastOccurrenceBetween returns the schedule's latest occurrence after from and at or
// before cutoff, or the zero time when that window holds none. A schedule that has run out
// reports the zero time, which is never after the cutoff, so the walk stops on it rather
// than asking for a step that cannot come.
func findLastOccurrenceBetween(cron river.PeriodicSchedule, from, cutoff time.Time) time.Time {
	occurrence := cron.Next(from)
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
