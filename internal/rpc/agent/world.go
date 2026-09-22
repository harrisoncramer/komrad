package agent

import (
	"context"

	"weave/internal/assemble"
	"weave/internal/fail"
	"weave/internal/rpc/render"
	"weave/internal/rpcerr"
	"weave/lib/xslice"

	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	pullrequeststore "weave/internal/store/pullrequest"
	rowstore "weave/internal/store/row"
	workflowstore "weave/internal/store/workflow"
	agentv1 "weave/proto/gen/agent/v1"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
	workflowv1 "weave/proto/gen/workflow/v1"
)

type workflowReader interface {
	ListByID(ctx context.Context, ids []string) ([]workflowstore.Workflow, error)
}

type unclaimedMailReader interface {
	ListUnclaimedTo(ctx context.Context, agentIDs []string) ([]mailstore.Mail, error)
}

type pullRequestReader interface {
	ListForAgents(ctx context.Context, agentIDs []string) ([]pullrequeststore.PullRequest, error)
}

// agentRow is an assembled row with the keyset cursor locating it.
type agentRow struct {
	*agentv1.AgentRow
	cursor agentstore.Cursor
}

// Cursor returns the keyset cursor locating this row.
func (r agentRow) Cursor() (agentstore.Cursor, error) {
	return r.cursor, nil
}

// assembleRows draws the rows of a page of agents: the agents themselves and, read beside
// them, every mark, the unclaimed mail to them, the pull requests on their branches and their
// workflows, joined by the assembler.
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
	agentIDs := xslice.Map(rows, func(row rowstore.Row) string { return row.ID })
	workflowIDs := xslice.Map(rows, func(row rowstore.Row) string { return row.WorkflowID })

	marks, err := s.marks.ListMarks(ctx)
	if err != nil {
		return assemble.World{}, rpcerr.From(err, "failed to list the marks")
	}

	mail, err := s.unclaimed.ListUnclaimedTo(ctx, agentIDs)
	if err != nil {
		return assemble.World{}, rpcerr.From(err, "failed to list the unclaimed mail")
	}

	pullRequests, err := s.pullRequests.ListForAgents(ctx, agentIDs)
	if err != nil {
		return assemble.World{}, rpcerr.From(err, "failed to list the pull requests")
	}

	workflows, err := s.workflows.ListByID(ctx, workflowIDs)
	if err != nil {
		return assemble.World{}, rpcerr.From(err, "failed to list the workflows")
	}

	agents := xslice.Map(rows, func(row rowstore.Row) *agentv1.Agent { return render.Agent(row, options) })

	return assemble.World{
		Agents: xslice.KeyBy(agents, func(a *agentv1.Agent) string { return a.GetId() }),
		Workflows: xslice.KeyBy(xslice.Map(workflows, func(w workflowstore.Workflow) *workflowv1.Workflow {
			return render.Workflow(w, render.WorkflowOptions{})
		}), func(w *workflowv1.Workflow) string { return w.GetId() }),
		Marks: xslice.KeyBy(xslice.Map(marks, render.Mark), func(m *agentv1.Mark) string { return m.GetId() }),
		Mail:  xslice.KeyBy(xslice.Map(mail, render.Mail), func(m *agentv1.Mail) string { return m.GetId() }),
		PullRequests: xslice.KeyBy(xslice.Map(pullRequests, render.PullRequest), func(p *pullrequestv1.PullRequest) string {
			return p.GetId()
		}),
	}, nil
}
