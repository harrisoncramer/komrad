package schedules

import (
	"context"
	"errors"
	"fmt"

	"komrad/internal/store"
	schedulestore "komrad/internal/store/schedule"

	"github.com/riverqueue/river"
)

type periodicJobRegistrar interface {
	Add(id string, cron river.PeriodicSchedule) error
	Remove(id string)
}

type registration struct {
	cron     string
	timezone string
}

// ArmPeriodic registers every live schedule with River and keeps those registrations matching
// each schedule as it is written.
func (s *ScheduleService) ArmPeriodic(ctx context.Context, periodic periodicJobRegistrar) error {
	s.armMu.Lock()
	defer s.armMu.Unlock()

	s.periodic = periodic

	listed, err := s.store.ListWatched(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to list schedules to arm: %w", err)
	}

	var failures []error
	for _, schedule := range listed {
		if err := s.applyRegistration(&schedule); err != nil {
			failures = append(failures, err)
		}
	}

	return errors.Join(failures...)
}

// SyncRegistration rereads a schedule and makes its River registration match what it now holds.
func (s *ScheduleService) SyncRegistration(ctx context.Context, id string) error {
	s.armMu.Lock()
	defer s.armMu.Unlock()

	if s.periodic == nil {
		return nil
	}

	schedule, err := s.store.Get(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		s.disarm(id)

		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to reread schedule %q to register it: %w", id, err)
	}

	return s.applyRegistration(schedule)
}

// syncAfterWrite registers a committed write even when the caller has gone.
func (s *ScheduleService) syncAfterWrite(ctx context.Context, id string) error {
	return s.SyncRegistration(context.WithoutCancel(ctx), id)
}

func (s *ScheduleService) applyRegistration(schedule *schedulestore.Schedule) error {
	cron, ok := armableCron(schedule)
	if !ok {
		s.disarm(schedule.ID)

		return nil
	}

	want := registration{cron: schedule.Cron, timezone: schedule.Timezone}
	if have, registered := s.armed[schedule.ID]; registered && have == want {
		return nil
	}

	s.disarm(schedule.ID)

	if err := s.periodic.Add(schedule.ID, cron); err != nil {
		return fmt.Errorf("failed to register schedule %q: %w", schedule.ID, err)
	}

	s.armed[schedule.ID] = want

	return nil
}

func (s *ScheduleService) disarm(id string) {
	if _, registered := s.armed[id]; !registered {
		return
	}

	s.periodic.Remove(id)
	delete(s.armed, id)
}

// armableCron is the schedule's parsed cron, false for a schedule that is off or whose cron will
// not parse.
func armableCron(schedule *schedulestore.Schedule) (river.PeriodicSchedule, bool) {
	if !schedule.Enabled || schedule.ArchivedAt != nil {
		return nil, false
	}

	parsed, err := ParseCron(schedule.Cron, schedule.Timezone)

	return parsed, err == nil
}
