package agents

import (
	"context"
	"fmt"

	"komrad/internal/agenterr"
	agentstore "komrad/internal/store/agent"
	"komrad/lib/xslice"
)

// parentRoom is one parent and how many children a call wants to put under it.
type parentRoom struct {
	id   string
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

// claimRoomForChildren holds one parent to the children it may run at once, a lower ceiling
// than the panel's own.
func claimRoomForChildren(ctx context.Context, agents *agentstore.AgentStore, maxChildren int, parent *agentstore.Agent) error {
	if parent == nil || maxChildren <= 0 {
		return nil
	}
	if err := agents.LockSlots(ctx); err != nil {
		return err
	}

	return requireRoomForChildren(ctx, agents, maxChildren, []parentRoom{{
		id:   parent.ID,
		name: parent.Name,
		want: 1,
	}})
}

// claimRoomForRestoredChildren holds every parent a restoring batch hangs a child under to the
// ceiling a create is held to.
func claimRoomForRestoredChildren(ctx context.Context, agents *agentstore.AgentStore, maxChildren int, restoring []agentstore.Agent) error {
	if maxChildren <= 0 {
		return nil
	}

	// The whole batch is counted against each parent at once, which children checked one at a time
	// would walk past together.
	parentIDs, want := countWantedUnder(restoring, func(agent agentstore.Agent) *string { return agent.ParentAgentID })
	if len(parentIDs) == 0 {
		return nil
	}

	if err := agents.LockSlots(ctx); err != nil {
		return err
	}

	parents, err := agents.ListByID(ctx, parentIDs)
	if err != nil {
		return fmt.Errorf("failed to look up %d parents of the restoring children: %w", len(parentIDs), err)
	}

	return requireRoomForChildren(ctx, agents, maxChildren, roomsWanted(parents, want))
}

func claimRoomForSpawned(ctx context.Context, agents *agentstore.AgentStore, maxSpawned int, spawner *agentstore.Agent) error {
	if spawner == nil || spawner.Role != agentstore.RoleOrchestrator || maxSpawned <= 0 {
		return nil
	}
	if err := agents.LockSlots(ctx); err != nil {
		return err
	}

	return requireRoomForSpawned(ctx, agents, maxSpawned, []parentRoom{{
		id:   spawner.ID,
		name: spawner.Name,
		want: 1,
	}})
}

func claimRoomForRestoredSpawned(ctx context.Context, agents *agentstore.AgentStore, maxSpawned int, restoring []agentstore.Agent) error {
	if maxSpawned <= 0 {
		return nil
	}

	spawnerIDs, want := countWantedUnder(restoring, func(agent agentstore.Agent) *string {
		if agent.ParentAgentID != nil {
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
		return fmt.Errorf("failed to look up %d agents that spawned the restoring ones: %w", len(spawnerIDs), err)
	}

	orchestrators := xslice.Filter(spawners, func(spawner agentstore.Agent) bool {
		return spawner.Role == agentstore.RoleOrchestrator
	})

	return requireRoomForSpawned(ctx, agents, maxSpawned, roomsWanted(orchestrators, want))
}

func countWantedUnder(restoring []agentstore.Agent, under func(agentstore.Agent) *string) ([]string, map[string]int) {
	want := make(map[string]int, len(restoring))
	var ids []string
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

func roomsWanted(parents []agentstore.Agent, want map[string]int) []parentRoom {
	return xslice.Map(parents, func(parent agentstore.Agent) parentRoom {
		return parentRoom{
			id:   parent.ID,
			name: parent.Name,
			want: want[parent.ID],
		}
	})
}

func requireRoomForSpawned(ctx context.Context, agents *agentstore.AgentStore, maxSpawned int, wanted []parentRoom) error {
	for _, spawner := range wanted {
		held, err := agents.CountLiveSpawned(ctx, spawner.id)
		if err != nil {
			return fmt.Errorf("failed to count the agents spawned by %q: %w", spawner.name, err)
		}
		if held+spawner.want > maxSpawned {
			return fmt.Errorf("%w: %q may run %d agents it spawned at once and this would make %d",
				agenterr.ErrTooManySpawned, spawner.name, maxSpawned, held+spawner.want)
		}
	}

	return nil
}

func requireRoomForChildren(ctx context.Context, agents *agentstore.AgentStore, maxChildren int, wanted []parentRoom) error {
	for _, parent := range wanted {
		held, err := agents.CountLiveChildren(ctx, parent.id)
		if err != nil {
			return fmt.Errorf("failed to count the children of agent %q: %w", parent.name, err)
		}
		if held+parent.want > maxChildren {
			return fmt.Errorf("%w: %q may run %d at once and this would make %d, and they share one checkout",
				agenterr.ErrTooManyChildren, parent.name, maxChildren, held+parent.want)
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
