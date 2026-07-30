package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"weave/internal/agents"

	"github.com/riverqueue/river"
)

// periodicJobs is what the reconciler needs to keep River's registrations in step, stated
// in terms of the schedule id and the expression it fires on rather than River's handles,
// so a test can supply its own and assert on which schedules were armed and disarmed.
//
// The job itself is built by the implementation rather than handed in, since the id River
// files a registration under has to be the id it is later removed by, and passing a
// ready-made job leaves those free to disagree.
type periodicJobs interface {
	Add(id string, cron river.PeriodicSchedule) error
	Remove(id string)
}

// RiverPeriodic adapts a River client's periodic job bundle to what the
// scheduler drives.
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

// Scheduler keeps River's periodic jobs matching the schedules table.
//
// River holds its periodic jobs in memory and the schedules a person creates live in
// Postgres, so something has to carry one into the other. Every pass reads the table and
// makes the registered set match, so a schedule created through the panel, the CLI, or an
// MCP tool is picked up without any of them knowing this exists.
type Scheduler struct {
	schedules ScheduleReader
	periodic  periodicJobs
	// registered is what this has told River about, keyed by schedule id. The value is what
	// the job was built from, so a pass can tell a schedule that changed from one that only
	// had its name edited.
	registered map[string]registration
}

// registration is the part of a schedule the periodic job is built from. A change to any
// of it means the registered job no longer matches the row.
type registration struct {
	cron     string
	timezone string
}

// NewScheduler builds the reconciler over the schedules it reads and the periodic jobs it
// maintains.
func NewScheduler(schedules ScheduleReader, periodic periodicJobs) *Scheduler {
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
// A schedule whose expression cannot be parsed is skipped rather than failing the pass,
// since one bad row should not stop every other schedule from running. A registration
// River refuses is the same situation from the other side, so it is gathered and the
// pass carries on. Returning there would leave the schedules after it unregistered and
// skip the removal below, which is what actually takes a paused schedule off.
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

		// An expression that changed has to replace its registration rather than sit beside
		// it, or the schedule would fire on both the old one and the new. The entry goes
		// with it, so what this has told River about never claims a job River no longer
		// holds.
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

// periodicJobFor builds the River periodic job for one schedule, filed under the id
// it will be removed by. The occurrence is taken from the schedule rather than from
// the clock, so two inserts standing for the same firing carry identical arguments
// and River's unique index collapses them into one run.
func periodicJobFor(scheduleID string, cron river.PeriodicSchedule) *river.PeriodicJob {
	return river.NewPeriodicJob(
		cron,
		func() (river.JobArgs, *river.InsertOpts) {
			return LaunchAgentArgs{
				ScheduleID:   scheduleID,
				ScheduledFor: occurrenceFor(cron, time.Now()),
			}, nil
		},
		&river.PeriodicJobOpts{ID: scheduleID},
	)
}

// lateness is how long after a firing was due the job standing for it may still be
// built. River constructs the arguments when its enqueuer gets to the schedule, which
// is usually within a tick and occasionally much later: a leader election, a paused
// process, or a machine coming out of sleep all put the construction well after the
// moment the schedule fired. An hour is far beyond any of them, and it bounds the walk
// below to sixty steps at the finest resolution a cron expression can name.
const lateness = time.Hour

// skew is how far ahead of an occurrence the construction may run and still be counted
// as standing for it, covering a firing that lands a hair before the boundary.
const skew = 5 * time.Second

// occurrenceFor names the firing a job stands for: the schedule's last occurrence at or
// before now. Which firing it is has to come from the schedule, since the clock alone
// cannot tell a job built a minute late for one occurrence from one built early for the
// next, and naming the wrong one is not cosmetic. The run either collides with the
// previous firing on the unique index and vanishes with nothing recorded, or is filed
// under a firing that has not happened yet and blocks the one that follows.
//
// It walks forward rather than back because a cron schedule can only say what comes
// next, starting from a point far enough behind that no plausible lateness precedes it.
func occurrenceFor(cron river.PeriodicSchedule, now time.Time) time.Time {
	cutoff := now.Add(skew)

	occurrence := cron.Next(now.Add(-lateness))
	for {
		next := cron.Next(occurrence)
		if next.After(cutoff) {
			return occurrence
		}

		occurrence = next
	}
}
