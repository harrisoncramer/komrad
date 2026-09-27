package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"komrad/internal/agents"
	"komrad/internal/jobs/internaljobs"
	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"
	"komrad/lib/xslice"
	"komrad/lib/xstr"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

type AgentDelete struct {
	queue *Enqueuer
}

// NewAgentDelete builds the queuer of the jobs that delete agents.
func NewAgentDelete(queue *Enqueuer) *AgentDelete {
	return &AgentDelete{queue: queue}
}

// DeleteAgents claims agents and their children and queues a delete for each claimed root.
func (d *AgentDelete) DeleteAgents(ctx context.Context, ids []string) error {
	return d.queue.InsertManyTx(ctx, func(tx pgx.Tx) ([]river.JobArgs, error) {
		store := agentstore.NewAgentStore(tx)

		found, err := store.ListByID(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("failed to look up %d agents: %w", len(ids), err)
		}
		if len(found) != len(ids) {
			held := xslice.KeyBy(found, func(agent agentstore.Agent) string { return agent.ID })
			missing := xslice.Filter(ids, func(id string) bool {
				_, ok := held[id]

				return !ok
			})

			return nil, agents.ErrAgentNotFound.Saying("no agent with id %s", xstr.JoinSome(missing, missingShown))
		}

		claims, err := store.MarkDeleting(ctx, ids)
		if err != nil {
			return nil, err
		}

		return deleteAgentJobs(agentstore.ClaimRoots(claims)), nil
	})
}

// Resume queues a delete for every claimed root no waiting job covers.
func (d *AgentDelete) Resume(ctx context.Context) error {
	waiting, err := d.queue.ListWaiting(ctx, internaljobs.DeleteAgentArgs{}.Kind())
	if err != nil {
		return err
	}

	covered := make(map[string]bool, len(waiting))
	for _, job := range waiting {
		var args internaljobs.DeleteAgentArgs
		if err := json.Unmarshal(job.EncodedArgs, &args); err != nil {
			return fmt.Errorf("failed to read the arguments of job %d: %w", job.ID, err)
		}
		covered[claimKey(args.AgentID, args.Claim)] = true
	}

	return d.queue.InsertManyTx(ctx, func(tx pgx.Tx) ([]river.JobArgs, error) {
		if err := workflowstore.NewWorkflowStore(tx).ClearAbandonedDeleting(ctx); err != nil {
			return nil, err
		}

		roots, err := agentstore.NewAgentStore(tx).ListDeletingRoots(ctx)
		if err != nil {
			return nil, err
		}

		return deleteAgentJobs(xslice.Filter(roots, func(root agentstore.Claim) bool { return !covered[claimKey(root.ID, root.DeletingAt)] })), nil
	})
}

// missingShown is how many missing ids a refusal names before it counts the rest.
const missingShown = 5

func deleteAgentJobs(roots []agentstore.Claim) []river.JobArgs {
	return xslice.Map(roots, func(root agentstore.Claim) river.JobArgs { return deleteAgentArgs(root) })
}

func claimKey(id string, claim time.Time) string {
	return id + "@" + claim.UTC().Format(time.RFC3339Nano)
}

func deleteAgentArgs(root agentstore.Claim) internaljobs.DeleteAgentArgs {
	return internaljobs.DeleteAgentArgs{AgentID: root.ID, Claim: root.DeletingAt.UTC()}
}
