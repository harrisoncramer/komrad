package agent

import (
	"context"
	"fmt"

	"komrade/internal/store"
)

// CountLive returns the number of unarchived agents.
func (s *AgentStore) CountLive(ctx context.Context) (int64, error) {
	count, err := s.queries.CountLiveAgents(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to count the live agents: %w", err)
	}

	return count, nil
}

// LockSlots serializes the current engine's agent slot claims for the current transaction.
func (s *AgentStore) LockSlots(ctx context.Context) error {
	if err := s.queries.LockAgentSlots(ctx); err != nil {
		return fmt.Errorf("failed to lock the agent slots: %w", err)
	}

	return nil
}

// CountLiveForSchedule returns the number of non-idle agents launched by a schedule.
func (s *AgentStore) CountLiveForSchedule(ctx context.Context, scheduleID string) (int64, error) {
	if !store.IsID(scheduleID) {
		return 0, nil
	}

	count, err := s.queries.CountLiveScheduleAgents(ctx, &scheduleID)
	if err != nil {
		return 0, fmt.Errorf("failed to count the live agents of schedule %q: %w", scheduleID, err)
	}

	return count, nil
}
