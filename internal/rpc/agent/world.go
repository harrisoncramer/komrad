package agent

import (
	"context"

	"weave/internal/assemble"
	"weave/internal/fail"
	"weave/internal/rpc/render"
	"weave/internal/rpcerr"
	"weave/lib/xptr"
	"weave/lib/xslice"

	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	markstore "weave/internal/store/mark"
	pullrequeststore "weave/internal/store/pullrequest"
	reviewstore "weave/internal/store/review"
	rowstore "weave/internal/store/row"
	workflowstore "weave/internal/store/workflow"
	agentv1 "weave/proto/gen/agent/v1"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
	workflowv1 "weave/proto/gen/workflow/v1"
)

type watchedLister[R any] interface {
	ListWatched(ctx context.Context, keys []string) ([]R, error)
}

type pullRequestLister interface {
	watchedLister[pullrequeststore.PullRequest]
	ListObservations(ctx context.Context, agentIDs []string) ([]pullrequeststore.Observation, error)
}

type reviewLister interface {
	watchedLister[reviewstore.Review]
	ListIDsOfAgents(ctx context.Context, agentIDs []string) ([]string, error)
}

// WatchedListers read the watched sets a page of agents is drawn from, the same ones the watch streams.
type WatchedListers struct {
	Marks        watchedLister[markstore.Mark]
	Mail         watchedLister[mailstore.Mail]
	PullRequests pullRequestLister
	Repositories watchedLister[pullrequeststore.Sync]
	Reviews      reviewLister
	Workflows    watchedLister[workflowstore.Workflow]
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
	agentIDs := xslice.Map(rows, func(row rowstore.Row) string { return row.ID })

	marks, err := s.watched.Marks.ListWatched(ctx, nil)
	if err != nil {
		return assemble.World{}, rpcerr.From(err, "failed to list the marks")
	}

	mail, err := s.watched.Mail.ListWatched(ctx, nil)
	if err != nil {
		return assemble.World{}, rpcerr.From(err, "failed to list the unclaimed mail")
	}

	observations, err := s.watched.PullRequests.ListObservations(ctx, agentIDs)
	if err != nil {
		return assemble.World{}, rpcerr.From(err, "failed to list the pull requests the agents were seen with")
	}

	pullRequests, err := listWatchedByKey(ctx, s.watched.PullRequests, xslice.Map(observations, func(o pullrequeststore.Observation) string {
		return o.PullRequestID
	}))
	if err != nil {
		return assemble.World{}, rpcerr.From(err, "failed to list the pull requests")
	}

	repositories, err := listWatchedByKey(ctx, s.watched.Repositories, xslice.Map(rows, func(row rowstore.Row) string {
		return xptr.Deref(row.RepositoryURL)
	}))
	if err != nil {
		return assemble.World{}, rpcerr.From(err, "failed to list the repositories")
	}

	reviewIDs, err := s.watched.Reviews.ListIDsOfAgents(ctx, agentIDs)
	if err != nil {
		return assemble.World{}, rpcerr.From(err, "failed to list the reviews of the agents")
	}

	reviews, err := listWatchedByKey(ctx, s.watched.Reviews, reviewIDs)
	if err != nil {
		return assemble.World{}, rpcerr.From(err, "failed to list the reviews")
	}

	workflows, err := listWatchedByKey(ctx, s.watched.Workflows, xslice.Map(rows, func(row rowstore.Row) string { return row.WorkflowID }))
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
		Repositories: xslice.KeyBy(xslice.Map(repositories, render.Repository), func(r *pullrequestv1.Repository) string {
			return r.GetUrl()
		}),
		Reviews: xslice.KeyBy(xslice.Map(reviews, render.WatchedReview), func(r *agentv1.WatchedReview) string {
			return r.GetId()
		}),
	}, nil
}

// listWatchedByKey reads the watched rows among keys, and nothing when there are none.
func listWatchedByKey[R any](ctx context.Context, lister watchedLister[R], keys []string) ([]R, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	return lister.ListWatched(ctx, keys)
}
