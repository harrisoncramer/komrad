package agent

import (
	"context"
	"fmt"

	"github.com/google/uuid"
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
func (s *AgentStore) CountLiveForSchedule(ctx context.Context, scheduleID uuid.UUID) (int64, error) {
	count, err := s.queries.CountLiveScheduleAgents(ctx, &scheduleID)
	if err != nil {
		return 0, fmt.Errorf("failed to count the live agents of schedule %q: %w", scheduleID, err)
	}

	return count, nil
}
