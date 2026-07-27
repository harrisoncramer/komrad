package jobs

import (
	"context"
	"fmt"
	"time"

	"claude-agents/internal/agents"

	"github.com/riverqueue/river"
)

// periodicJobs is what the reconciler needs to keep River's registrations in
// step, stated in terms of the schedule id rather than River's handles. River's
// own bundle is wrapped to fit it by RiverPeriodic, and a test supplies its own,
// since what matters here is which schedules were armed and disarmed rather than
// that River ran them.
type periodicJobs interface {
	Add(id string, job *river.PeriodicJob) error
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

// Add registers a periodic job under an id it can later be removed by.
func (r *RiverPeriodic) Add(_ string, job *river.PeriodicJob) error {
	if _, err := r.bundle.AddSafely(job); err != nil {
		return fmt.Errorf("failed to add a periodic job: %w", err)
	}

	return nil
}

// Remove takes a registration back off, and is a no-op when nothing holds that
// id.
func (r *RiverPeriodic) Remove(id string) {
	r.bundle.RemoveByID(id)
}

// Scheduler keeps River's periodic jobs matching the schedules table.
//
// River holds its periodic jobs in memory, and the schedules a person creates
// live in Postgres, so something has to carry one into the other. This does it by
// reconciling rather than by being told. Every pass reads the table and makes the
// registered set match, so a schedule created through the panel, the CLI, or an
// MCP tool is picked up without any of them knowing this exists.
type Scheduler struct {
	schedules ScheduleReader
	periodic  periodicJobs
	// registered is what this has told River about, keyed by schedule id. The
	// value is what the job was built from, so a pass can tell a schedule that
	// changed from one that only had its name edited.
	registered map[string]registration
}

// registration is the part of a schedule the periodic job is built from. A
// change to any of it means the registered job no longer matches the row.
type registration struct {
	cron     string
	timezone string
}

// NewScheduler builds the reconciler over the schedules it reads and the
// periodic jobs it maintains.
func NewScheduler(schedules ScheduleReader, periodic periodicJobs) *Scheduler {
	return &Scheduler{
		schedules:  schedules,
		periodic:   periodic,
		registered: make(map[string]registration),
	}
}

// Reconcile makes River's periodic jobs match the schedules table. Enabled
// schedules are registered, paused and deleted ones are removed, and a schedule
// whose expression changed is re-registered against the new one.
//
// A schedule whose expression cannot be parsed is skipped rather than failing
// the pass. The API refuses one at write time, so this only happens to a row
// written before a rule changed, and one bad row should not stop every other
// schedule from running.
func (s *Scheduler) Reconcile(ctx context.Context) error {
	listed, err := s.schedules.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list schedules to reconcile: %w", err)
	}

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

		// An expression that changed has to replace its registration rather than
		// sit beside it, or the schedule would fire on both the old one and the
		// new. A schedule seen for the first time has nothing to replace.
		if registered {
			s.periodic.Remove(schedule.ID)
		}

		if err := s.periodic.Add(schedule.ID, periodicJobFor(schedule.ID, cron)); err != nil {
			return fmt.Errorf("failed to register schedule %q: %w", schedule.ID, err)
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

	return nil
}

// periodicJobFor builds the River periodic job for one schedule. The occurrence
// is stamped when the job is constructed, at the resolution a cron expression
// fires at, so two inserts for the same minute carry identical arguments and
// River's unique index collapses them into one run.
func periodicJobFor(scheduleID string, cron river.PeriodicSchedule) *river.PeriodicJob {
	return river.NewPeriodicJob(
		cron,
		func() (river.JobArgs, *river.InsertOpts) {
			return LaunchAgentArgs{
				ScheduleID:   scheduleID,
				ScheduledFor: occurrenceAt(time.Now()),
			}, nil
		},
		&river.PeriodicJobOpts{ID: scheduleID},
	)
}

// occurrenceAt names the minute a firing belongs to, rounding to the nearest one
// rather than down.
//
// The job is constructed as the schedule fires, and that can happen a hair
// either side of the boundary. Rounding down turns a job constructed at
// 09:59.998 into the 09:59 occurrence, which is the minute before's, and the
// unique index then swallows it as a duplicate of a run that already happened.
// The run disappears with nothing recorded anywhere. Rounding gives half a
// minute of slack in both directions, which is far more than the scheduler is
// ever out by.
func occurrenceAt(t time.Time) time.Time {
	return t.Add(30 * time.Second).Truncate(time.Minute)
}
