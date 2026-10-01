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
		if err := s.applyRegistration(schedule.ID, &schedule); err != nil {
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
		return s.applyRegistration(id, nil)
	}
	if err != nil {
		return fmt.Errorf("failed to reread schedule %q to register it: %w", id, err)
	}

	return s.applyRegistration(id, schedule)
}

func (s *ScheduleService) applyRegistration(id string, schedule *schedulestore.Schedule) error {
	if !isArmable(schedule) {
		s.disarm(id)

		return nil
	}

	cron, err := ParseCron(schedule.Cron, schedule.Timezone)
	if err != nil {
		s.disarm(id)

		return nil
	}

	want := registration{cron: schedule.Cron, timezone: schedule.Timezone}
	if have, registered := s.armed[id]; registered && have == want {
		return nil
	}

	s.disarm(id)

	if err := s.periodic.Add(id, cron); err != nil {
		return fmt.Errorf("failed to register schedule %q: %w", id, err)
	}

	s.armed[id] = want

	return nil
}

func (s *ScheduleService) disarm(id string) {
	if _, registered := s.armed[id]; !registered {
		return
	}

	s.periodic.Remove(id)
	delete(s.armed, id)
}

func isArmable(schedule *schedulestore.Schedule) bool {
	return schedule != nil && schedule.Enabled && schedule.ArchivedAt == nil
}
