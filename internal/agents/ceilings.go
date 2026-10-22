package agents

import (
	"context"
	"fmt"

	"komrad/internal/agenterr"
	agentstore "komrad/internal/store/agent"
	"komrad/lib/xslice"

	"github.com/google/uuid"
)

// room is one task or orchestrator and how many agents a call wants to put under it.
type room struct {
	id   uuid.UUID
	name string
	want int
}

func claimRoomForAgents(ctx context.Context, agents *agentstore.AgentStore, maxAgents, want int) error {
	if maxAgents <= 0 || want <= 0 {
		return nil
	}
	if err := agents.LockSlots(ctx); err != nil {
		return err
	}

	return requireRoomForAgents(ctx, agents, maxAgents, want)
}

// claimRoomForReviewer holds one task to the reviewers it may run at once, a lower ceiling than the
// panel's own.
func claimRoomForReviewer(ctx context.Context, agents *agentstore.AgentStore, maxReviewers int, taskID uuid.UUID) error {
	if maxReviewers <= 0 {
		return nil
	}
	if err := agents.LockSlots(ctx); err != nil {
		return err
	}

	return requireRoomForReviewers(ctx, agents, maxReviewers, []room{{
		id:   taskID,
		want: 1,
	}})
}

// claimRoomForRestoredReviewers holds every task a restoring batch puts a reviewer back on to the
// ceiling a create is held to.
func claimRoomForRestoredReviewers(ctx context.Context, agents *agentstore.AgentStore, maxReviewers int, restoring []agentstore.Agent) error {
	if maxReviewers <= 0 {
		return nil
	}

	// The whole batch is counted against each task at once, which reviewers checked one at a time
	// would walk past together.
	taskIDs, want := countWantedUnder(restoring, func(agent agentstore.Agent) *uuid.UUID {
		if agent.Role != agentstore.RoleReviewer {
			return nil
		}

		return agent.TaskID
	})
	if len(taskIDs) == 0 {
		return nil
	}

	if err := agents.LockSlots(ctx); err != nil {
		return err
	}

	return requireRoomForReviewers(ctx, agents, maxReviewers, xslice.Map(taskIDs, func(id uuid.UUID) room {
		return room{
			id:   id,
			want: want[id],
		}
	}))
}

func claimRoomForTask(ctx context.Context, agents *agentstore.AgentStore, maxTasks int, spawner *agentstore.Agent) error {
	if spawner == nil || spawner.Role != agentstore.RoleOrchestrator || maxTasks <= 0 {
		return nil
	}
	if err := agents.LockSlots(ctx); err != nil {
		return err
	}

	return requireRoomForTasks(ctx, agents, maxTasks, []room{{
		id:   spawner.ID,
		name: spawner.Name,
		want: 1,
	}})
}

func claimRoomForRestoredTasks(ctx context.Context, agents *agentstore.AgentStore, maxTasks int, restoring []agentstore.Agent) error {
	if maxTasks <= 0 {
		return nil
	}

	spawnerIDs, want := countWantedUnder(restoring, func(agent agentstore.Agent) *uuid.UUID {
		if agent.Role != agentstore.RoleWriter {
			return nil
		}

		return agent.SpawnedByAgentID
	})
	if len(spawnerIDs) == 0 {
		return nil
	}

	if err := agents.LockSlots(ctx); err != nil {
		return err
	}

	spawners, err := agents.ListByID(ctx, spawnerIDs)
	if err != nil {
		return fmt.Errorf("failed to look up %d agents that started the restoring tasks: %w", len(spawnerIDs), err)
	}

	orchestrators := xslice.Filter(spawners, func(spawner agentstore.Agent) bool {
		return spawner.Role == agentstore.RoleOrchestrator
	})

	return requireRoomForTasks(ctx, agents, maxTasks, xslice.Map(orchestrators, func(orchestrator agentstore.Agent) room {
		return room{
			id:   orchestrator.ID,
			name: orchestrator.Name,
			want: want[orchestrator.ID],
		}
	}))
}

func countWantedUnder(restoring []agentstore.Agent, under func(agentstore.Agent) *uuid.UUID) ([]uuid.UUID, map[uuid.UUID]int) {
	want := make(map[uuid.UUID]int, len(restoring))
	var ids []uuid.UUID
	for _, agent := range restoring {
		id := under(agent)
		if id == nil {
			continue
		}
		if want[*id] == 0 {
			ids = append(ids, *id)
		}
		want[*id]++
	}

	return ids, want
}

func requireRoomForTasks(ctx context.Context, agents *agentstore.AgentStore, maxTasks int, wanted []room) error {
	for _, orchestrator := range wanted {
		held, err := agents.CountLiveTasksStartedBy(ctx, orchestrator.id)
		if err != nil {
			return fmt.Errorf("failed to count the tasks started by %q: %w", orchestrator.name, err)
		}
		if held+orchestrator.want > maxTasks {
			return fmt.Errorf("%w: %q may run %d tasks it started at once and this would make %d",
				agenterr.ErrTooManyTasks, orchestrator.name, maxTasks, held+orchestrator.want)
		}
	}

	return nil
}

func requireRoomForReviewers(ctx context.Context, agents *agentstore.AgentStore, maxReviewers int, wanted []room) error {
	for _, task := range wanted {
		held, err := agents.CountLiveReviewers(ctx, task.id)
		if err != nil {
			return fmt.Errorf("failed to count the reviewers of task %s: %w", task.id, err)
		}
		if held+task.want > maxReviewers {
			return fmt.Errorf("%w: task %s may hold %d at once and this would make %d, and they share one checkout",
				agenterr.ErrTooManyReviewers, task.id, maxReviewers, held+task.want)
		}
	}

	return nil
}

func requireRoomForAgents(ctx context.Context, agents *agentstore.AgentStore, maxAgents, want int) error {
	if maxAgents <= 0 || want <= 0 {
		return nil
	}

	live, err := agents.CountLive(ctx)
	if err != nil {
		return fmt.Errorf("failed to count the agents already running: %w", err)
	}
	if live+int64(want) > int64(maxAgents) {
		return fmt.Errorf("%w: max number of concurrent agents reached (%d of %d), please archive one to continue", agenterr.ErrTooManyAgents, live, maxAgents)
	}

	return nil
}
