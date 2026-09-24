package agent

import (
	"context"

	"komrade/internal/assemble"
	"komrade/internal/fail"
	"komrade/internal/rpc/render"
	"komrade/internal/rpcerr"
	"komrade/lib/xslice"

	agentstore "komrade/internal/store/agent"
	rowstore "komrade/internal/store/row"
	workflowstore "komrade/internal/store/workflow"
	agentv1 "komrade/proto/gen/agent/v1"
	pullrequestv1 "komrade/proto/gen/pullrequest/v1"
	workflowv1 "komrade/proto/gen/workflow/v1"
)

// agentRow is an assembled row with the keyset cursor locating it.
type agentRow struct {
	*agentv1.AgentRow
	cursor agentstore.Cursor
}

// Cursor returns the keyset cursor locating this row.
func (r agentRow) Cursor() (agentstore.Cursor, error) {
	return r.cursor, nil
}

// assembleRows draws the rows of a page of agents from the watched sets around them, joined by the assembler.
func (s *Handler) assembleRows(ctx context.Context, rows []rowstore.Row, options render.AgentOptions) ([]agentRow, *fail.Error) {
	world, failure := s.world(ctx, rows, options)
	if failure != nil {
		return nil, failure
	}

	return xslice.Map(assemble.AgentRows(world, s.now()), func(row *agentv1.AgentRow) agentRow {
		return agentRow{AgentRow: row, cursor: agentstore.Cursor{ID: row.GetAgent().GetId()}}
	}), nil
}

func (s *Handler) world(ctx context.Context, rows []rowstore.Row, options render.AgentOptions) (assemble.World, *fail.Error) {
	surroundings, err := s.rows.ListSurroundings(ctx, rows)
	if err != nil {
		return assemble.World{}, rpcerr.From(err, "failed to list what the agents are drawn beside")
	}

	agents := xslice.Map(rows, func(row rowstore.Row) *agentv1.Agent { return render.Agent(row, options) })

	return assemble.World{
		Agents: xslice.KeyBy(agents, func(a *agentv1.Agent) string { return a.GetId() }),
		Workflows: xslice.KeyBy(xslice.Map(surroundings.Workflows, func(w workflowstore.Workflow) *workflowv1.Workflow {
			return render.Workflow(w, render.WorkflowOptions{})
		}), func(w *workflowv1.Workflow) string { return w.GetId() }),
		Marks: xslice.KeyBy(xslice.Map(surroundings.Marks, render.Mark), func(m *agentv1.Mark) string { return m.GetId() }),
		Mail:  xslice.KeyBy(xslice.Map(surroundings.Mail, render.Mail), func(m *agentv1.Mail) string { return m.GetId() }),
		PullRequests: xslice.KeyBy(xslice.Map(surroundings.PullRequests, render.PullRequest), func(p *pullrequestv1.PullRequest) string {
			return p.GetId()
		}),
		Repositories: xslice.KeyBy(xslice.Map(surroundings.Repositories, render.Repository), func(r *pullrequestv1.Repository) string {
			return r.GetUrl()
		}),
		Reviews: xslice.KeyBy(xslice.Map(surroundings.Reviews, render.WatchedReview), func(r *agentv1.WatchedReview) string {
			return r.GetId()
		}),
	}, nil
}
