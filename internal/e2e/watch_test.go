//go:build integration

package e2e

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"komrade/internal/assemble"
	"komrade/internal/client"
	"komrade/internal/komradetest"
	"komrade/internal/rpc/render"
	"komrade/internal/storetest"
	"komrade/lib/xptr"
	"komrade/lib/xslice"

	agentstore "komrade/internal/store/agent"
	mailstore "komrade/internal/store/mail"
	markstore "komrade/internal/store/mark"
	pullrequeststore "komrade/internal/store/pullrequest"
	reviewstore "komrade/internal/store/review"
	rowstore "komrade/internal/store/row"
	schedulestore "komrade/internal/store/schedule"
	workflowstore "komrade/internal/store/workflow"
	agentv1 "komrade/proto/gen/agent/v1"
	pullrequestv1 "komrade/proto/gen/pullrequest/v1"
	schedulev1 "komrade/proto/gen/schedule/v1"
	viewv1 "komrade/proto/gen/view/v1"
	workflowv1 "komrade/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	watchedRepository = "https://github.com/harrisoncramer/komrade"
	agreeWithin       = 10 * time.Second
)

// TestIntegrationTheWatchAndTheStoresAgree is the proof that the agent row has one definition: the world
// the panel fills off the watch, the world the agent handler reads from the stores, and what
// the list and get RPCs answer all assemble to the same rows, and a change written after the
// stream opened brings the watched world back to the stores through the events alone.
func TestIntegrationTheWatchAndTheStoresAgree(t *testing.T) {
	instance := komradetest.Start(t, komradetest.Params{})
	ctx := t.Context()
	st := instance.Stores

	// Two workflows, one cutting worktrees from main, and a schedule in the first.
	pipeline := st.SeedWorkflow(t, workflowstore.Config{Name: "pipeline", Context: "ship the pipeline"})
	docs := st.SeedWorkflow(t, workflowstore.Config{Name: "docs"})
	st.SeedSchedule(t, schedulestore.Config{
		Name:       "nightly",
		Cron:       "0 0 1 1 *",
		Timezone:   "UTC",
		Prompt:     "sweep",
		WorkflowID: pipeline.ID,
		Enabled:    true,
	})

	// Agents on a branch with pull requests, on the base branch, on a read repository with no
	// pull request, off any forge, and one archived.
	api := agentOn(t, st, pipeline.ID, "api", watchedRepository, "agent/api")
	base := agentOn(t, st, pipeline.ID, "base", watchedRepository, "main")
	quiet := agentOn(t, st, docs.ID, "quiet", watchedRepository, "agent/quiet")
	st.SeedAgent(t, agentstore.Agent{Name: "local", WorkflowID: docs.ID})
	gone := st.SeedAgent(t, agentstore.Agent{Name: "gone", WorkflowID: docs.ID})
	_, err := st.Agents.Archive(ctx, []string{gone.ID})
	require.NoError(t, err)

	_, err = st.Agents.SetMetadata(ctx, agentstore.SetMetadataParams{
		ID:    api.ID,
		Key:   "ticket",
		Value: xptr.Ptr("https://example.test/T-1"),
	})
	require.NoError(t, err)

	// A mark on an agent and one on a workflow.
	_, err = st.Marks.Set(ctx, "a", markstore.OnAgent(api.ID))
	require.NoError(t, err)
	_, err = st.Marks.Set(ctx, "d", markstore.OnWorkflow(docs.ID))
	require.NoError(t, err)

	// Mail waiting for one agent, and mail another has already claimed.
	_, err = st.Mail.Send(ctx, mailstore.SendParams{
		FromName:  "panel",
		ToAgentID: api.ID,
		Body:      "waiting",
		Deadline:  time.Hour,
	})
	require.NoError(t, err)
	_, err = st.Mail.Send(ctx, mailstore.SendParams{
		FromName:  "panel",
		ToAgentID: quiet.ID,
		Body:      "claimed",
		Deadline:  time.Hour,
	})
	require.NoError(t, err)
	_, err = st.Mail.Claim(ctx, quiet.ID, 10)
	require.NoError(t, err)

	// The repository has been swept, with two pull requests on api's branch, the older one
	// merged, one on a branch no live agent is on, and one on the base branch itself.
	require.NoError(t, st.PullRequestSyncs.EnsureSync(ctx, watchedRepository))
	require.NoError(t, st.PullRequestSyncs.StartSweep(ctx, pullrequeststore.StartSweepParams{
		RepositoryURL: watchedRepository,
		Newest:        time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC),
		ListedAt:      time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC),
	}))
	require.NoError(t, st.PullRequestSyncs.CompleteSweep(ctx, watchedRepository))
	openPR := upsertPullRequest(t, st, 2, "agent/api", nil)
	upsertPullRequest(t, st, 1, "agent/api", xptr.Ptr(time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)))
	upsertPullRequest(t, st, 3, "agent/nobody", nil)
	upsertPullRequest(t, st, 4, "main", nil)
	_, err = st.PullRequests.Observe(ctx)
	require.NoError(t, err)

	// The stores draw the fixture, so agreement below is over rows that hold something.
	seeded := viewOf(worldFromStores(t, st), time.Now())
	require.Equal(t, "a", seeded.agents[api.ID].GetMark())
	require.True(t, seeded.agents[api.ID].GetUnreadMail())
	require.Len(t, seeded.agents[api.ID].GetPullRequests(), 2)
	require.Equal(t, int32(2), seeded.agents[api.ID].GetPullRequest().GetNumber(), "the open pull request is current")
	require.Equal(t, "ticket", seeded.agents[api.ID].GetAgent().GetMetadata()[0].GetKey())
	require.Empty(t, seeded.agents[base.ID].GetPullRequests(), "a pull request on the base branch is nobody's")
	require.False(t, seeded.agents[quiet.ID].GetUnreadMail())
	require.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE, seeded.agents[quiet.ID].GetPullRequest().GetState())
	require.Equal(t, "d", seeded.marks[docs.ID])
	require.NotEmpty(t, seeded.agents[gone.ID].GetAgent().GetArchivedAt(), "the archived agent is watched as archived")
	require.Len(t, seeded.schedules, 1)

	// The stream opens with the snapshot, applied the way the panel applies it.
	batches := openWatch(t, instance)
	watched := &assemble.World{}
	applyBatch(watched, receiveBatch(t, batches))

	requireAgreement(t, instance, watched)

	tests := []struct {
		name  string
		write func(t *testing.T)
		took  func(t *testing.T, stores view)
	}{
		{
			name: "a mark moves from an agent to a workflow",
			write: func(t *testing.T) {
				_, err := instance.Agents.SetMark(ctx, connect.NewRequest(&agentv1.SetMarkRequest{
					Target: &agentv1.SetMarkRequest_WorkflowId{WorkflowId: pipeline.ID},
					Mark:   xptr.Ptr("a"),
				}))
				require.NoError(t, err)
			},
			took: func(t *testing.T, stores view) {
				assert.Nil(t, stores.agents[api.ID].Mark)
				assert.Equal(t, "a", stores.marks[pipeline.ID])
			},
		},
		{
			name: "a mail is claimed",
			write: func(t *testing.T) {
				req := connect.NewRequest(&agentv1.ReadMailRequest{})
				callAs(req, api.ID)
				_, err := instance.Agents.ReadMail(ctx, req)
				require.NoError(t, err)
			},
			took: func(t *testing.T, stores view) {
				assert.False(t, stores.agents[api.ID].GetUnreadMail())
			},
		},
		{
			name: "an agent moves onto a branch and is seen with its pull request",
			write: func(t *testing.T) {
				require.NoError(t, st.Agents.SetCheckout(ctx, quiet.ID, agentstore.SetCheckoutParams{
					Branch:        xptr.Ptr("agent/nobody"),
					RepositoryURL: xptr.Ptr(watchedRepository),
				}))
				_, err := st.PullRequests.Observe(ctx)
				require.NoError(t, err)
			},
			took: func(t *testing.T, stores view) {
				assert.Equal(t, int32(3), stores.agents[quiet.ID].GetPullRequest().GetNumber())
			},
		},
		{
			name: "a pull request is deleted",
			write: func(t *testing.T) {
				_, err := st.Pool.GetRawPool().Exec(ctx, "DELETE FROM pull_requests WHERE id = $1", openPR)
				require.NoError(t, err)
			},
			took: func(t *testing.T, stores view) {
				assert.Len(t, stores.agents[api.ID].GetPullRequests(), 1)
				assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED, stores.agents[api.ID].GetPullRequest().GetState())
			},
		},
		{
			name: "a review of an agent's work is released",
			write: func(t *testing.T) {
				reviews := reviewstore.NewReviewStore(st.Pool.GetDB())
				round, err := reviews.CreateRound(ctx, reviewstore.CreateRoundParams{AgentID: api.ID, BaseSHA: "base", SnapshotSHA: "snapshot"})
				require.NoError(t, err)
				review, err := reviews.OpenReview(ctx, reviewstore.OpenReviewParams{AgentID: api.ID, RoundID: round.ID, AuthorKind: reviewstore.AuthorHuman})
				require.NoError(t, err)
				_, err = reviews.ReleaseReview(ctx, reviewstore.ReleaseReviewParams{ID: review.ID, Verdict: reviewstore.VerdictApproved, Path: "/tmp/review.md"})
				require.NoError(t, err)
			},
			took: func(t *testing.T, stores view) {
				assert.Equal(t, int32(1), stores.agents[api.ID].GetLocalReviewRounds())
				assert.Equal(t, agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED, stores.agents[api.ID].GetLocalReviewVerdict())
			},
		},
		{
			name: "an agent is archived",
			write: func(t *testing.T) {
				_, err := instance.Agents.ArchiveAgents(ctx, connect.NewRequest(&agentv1.ArchiveAgentsRequest{Ids: []string{quiet.ID}}))
				require.NoError(t, err)
			},
			took: func(t *testing.T, stores view) {
				assert.NotEmpty(t, stores.agents[quiet.ID].GetAgent().GetArchivedAt())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.write(t)
			tt.took(t, viewOf(worldFromStores(t, st), time.Now()))
			awaitAgreement(t, instance, watched, batches)
		})
	}
}

// view is what every reader of the resources is compared on: the agent rows by id, the letter
// on each row, and the workflows and schedules by id.
type view struct {
	agents    map[string]*agentv1.AgentRow
	marks     map[string]string
	workflows map[string]*workflowv1.Workflow
	schedules map[string]*schedulev1.Schedule
}

// viewOf assembles a world.
func viewOf(w assemble.World, now time.Time) view {
	return view{
		agents:    xslice.KeyBy(assemble.AgentRows(w, now), func(row *agentv1.AgentRow) string { return row.GetAgent().GetId() }),
		marks:     assemble.Marks(w),
		workflows: w.Workflows,
		schedules: w.Schedules,
	}
}

// viewFromRPCs is what the list RPCs answer.
func viewFromRPCs(t *testing.T, instance *komradetest.Instance) view {
	t.Helper()
	ctx := t.Context()

	agents, err := instance.Agents.ListAgents(ctx, connect.NewRequest(&agentv1.ListAgentsRequest{View: viewv1.View_VIEW_FULL}))
	require.NoError(t, err)
	require.Empty(t, agents.Msg.GetNextPageToken(), "the live set fits one page")

	archived, err := instance.Agents.ListAgents(ctx, connect.NewRequest(&agentv1.ListAgentsRequest{View: viewv1.View_VIEW_FULL, Archived: true}))
	require.NoError(t, err)
	require.Empty(t, archived.Msg.GetNextPageToken(), "the archive fits one page")

	marks, err := instance.Agents.ListMarks(ctx, connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)

	workflows, err := instance.Workflows.ListWorkflows(ctx, connect.NewRequest(&workflowv1.ListWorkflowsRequest{View: viewv1.View_VIEW_FULL}))
	require.NoError(t, err)

	schedules, err := instance.Schedules.ListSchedules(ctx, connect.NewRequest(&schedulev1.ListSchedulesRequest{View: viewv1.View_VIEW_FULL}))
	require.NoError(t, err)

	return view{
		agents:    xslice.KeyBy(slices.Concat(agents.Msg.GetAgents(), archived.Msg.GetAgents()), func(row *agentv1.AgentRow) string { return row.GetAgent().GetId() }),
		marks:     assemble.Marks(assemble.World{Marks: xslice.KeyBy(marks.Msg.GetMarks(), func(m *agentv1.Mark) string { return m.GetMark() })}),
		workflows: xslice.KeyBy(workflows.Msg.GetWorkflows(), func(w *workflowv1.Workflow) string { return w.GetId() }),
		schedules: xslice.KeyBy(schedules.Msg.GetSchedules(), func(s *schedulev1.Schedule) string { return s.GetId() }),
	}
}

// viewFromGets is what GetAgent answers for each of the agents a view holds, beside the rest of
// that view.
func viewFromGets(t *testing.T, instance *komradetest.Instance, listed view) view {
	t.Helper()

	got := view{
		agents:    map[string]*agentv1.AgentRow{},
		marks:     listed.marks,
		workflows: listed.workflows,
		schedules: listed.schedules,
	}
	for id := range listed.agents {
		one, err := instance.Agents.GetAgent(t.Context(), connect.NewRequest(&agentv1.GetAgentRequest{Id: id}))
		require.NoError(t, err)
		got.agents[id] = one.Msg.GetAgent()
	}

	return got
}

// worldFromStores reads the watched set the way the agent handler reads a page: the live and
// archived rows, every mark, the unclaimed mail, the pull requests an agent was seen with,
// the repositories they sit on, the reviews of their work and their workflows, with the live
// schedules beside them.
func worldFromStores(t *testing.T, st *storetest.Stores) assemble.World {
	t.Helper()
	ctx := t.Context()

	live, err := st.Rows.ListPage(ctx, rowstore.Page{Limit: 100})
	require.NoError(t, err)
	archived, err := st.Rows.ListPage(ctx, rowstore.Page{Limit: 100, Archived: true})
	require.NoError(t, err)
	rows := slices.Concat(live, archived)
	agentIDs := xslice.Map(rows, func(row rowstore.Row) string { return row.ID })

	marks, err := st.Marks.ListWatched(ctx, nil)
	require.NoError(t, err)

	mail, err := st.Mail.ListWatched(ctx, nil)
	require.NoError(t, err)

	pullRequests, err := st.PullRequests.ListWatched(ctx, nil)
	require.NoError(t, err)

	repositories, err := st.PullRequestSyncs.ListWatched(ctx, xslice.Map(rows, func(row rowstore.Row) string { return xptr.Deref(row.RepositoryURL) }))
	require.NoError(t, err)

	reviewStore := reviewstore.NewReviewStore(st.Pool.GetDB())
	reviewIDs, err := reviewStore.ListIDsOfAgents(ctx, agentIDs)
	require.NoError(t, err)
	reviews, err := reviewStore.ListWatched(ctx, reviewIDs)
	require.NoError(t, err)

	workflows, err := st.Workflows.ListWatched(ctx, xslice.Map(rows, func(row rowstore.Row) string { return row.WorkflowID }))
	require.NoError(t, err)

	schedules, err := st.Schedules.ListPage(ctx, 100, nil)
	require.NoError(t, err)

	now := time.Now()

	return assemble.World{
		Agents: xslice.KeyBy(xslice.Map(rows, func(row rowstore.Row) *agentv1.Agent {
			return render.Agent(row, render.AgentOptions{})
		}), func(a *agentv1.Agent) string { return a.GetId() }),
		Workflows: xslice.KeyBy(xslice.Map(workflows, func(w workflowstore.Workflow) *workflowv1.Workflow {
			return render.Workflow(w, render.WorkflowOptions{})
		}), func(w *workflowv1.Workflow) string { return w.GetId() }),
		Schedules: xslice.KeyBy(xslice.Map(schedules, func(s schedulestore.Schedule) *schedulev1.Schedule {
			return render.Schedule(s, now, render.ScheduleOptions{})
		}), func(s *schedulev1.Schedule) string { return s.GetId() }),
		Marks: xslice.KeyBy(xslice.Map(marks, render.Mark), func(m *agentv1.Mark) string { return m.GetMark() }),
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
	}
}

// requireAgreement asserts the watched world, the stores, the list RPCs and GetAgent all say
// the same thing.
func requireAgreement(t *testing.T, instance *komradetest.Instance, watched *assemble.World) {
	t.Helper()

	now := time.Now()
	stores := viewOf(worldFromStores(t, instance.Stores), now)
	listed := viewFromRPCs(t, instance)

	require.Empty(t, differences("watch", viewOf(*watched, now), "stores", stores))
	require.Empty(t, differences("stores", stores, "list rpcs", listed))
	require.Empty(t, differences("list rpcs", listed, "get_agent", viewFromGets(t, instance, listed)))
}

// awaitAgreement applies batches until the watched world assembles to what the stores hold,
// then asserts the RPCs agree too.
func awaitAgreement(t *testing.T, instance *komradetest.Instance, watched *assemble.World, batches <-chan client.WatchBatch) {
	t.Helper()

	deadline := time.After(agreeWithin)
	for {
		now := time.Now()
		differing := differences("watch", viewOf(*watched, now), "stores", viewOf(worldFromStores(t, instance.Stores), now))
		if len(differing) == 0 {
			break
		}

		select {
		case batch := <-batches:
			applyBatch(watched, batch)
		case <-deadline:
			t.Fatalf("the watch never caught up with the stores:\n%s", strings.Join(differing, "\n"))
		}
	}

	requireAgreement(t, instance, watched)
}

// differences names every row and field on which two views disagree.
func differences(aName string, a view, bName string, b view) []string {
	var found []string
	report := func(kind, key, diff string) {
		found = append(found, fmt.Sprintf("%s %s (%s vs %s):\n%s", kind, key, aName, bName, diff))
	}

	for _, id := range keysOf(a.agents, b.agents) {
		if diff := cmp.Diff(a.agents[id], b.agents[id], protocmp.Transform()); diff != "" {
			report("agent", nameOf(a.agents[id], b.agents[id]), diff)
		}
	}
	for _, id := range keysOf(a.marks, b.marks) {
		if a.marks[id] != b.marks[id] {
			report("mark on", id, fmt.Sprintf("%q vs %q", a.marks[id], b.marks[id]))
		}
	}
	for _, id := range keysOf(a.workflows, b.workflows) {
		if diff := cmp.Diff(a.workflows[id], b.workflows[id], protocmp.Transform()); diff != "" {
			report("workflow", id, diff)
		}
	}
	for _, id := range keysOf(a.schedules, b.schedules) {
		if diff := cmp.Diff(a.schedules[id], b.schedules[id], protocmp.Transform()); diff != "" {
			report("schedule", id, diff)
		}
	}

	return found
}

// keysOf is the union of two maps' keys, sorted.
func keysOf[V any](a, b map[string]V) []string {
	keys := map[string]bool{}
	for key := range a {
		keys[key] = true
	}
	for key := range b {
		keys[key] = true
	}

	return slices.Sorted(maps.Keys(keys))
}

// nameOf is the name of whichever side holds the agent.
func nameOf(rows ...*agentv1.AgentRow) string {
	for _, row := range rows {
		if row != nil {
			return row.GetAgent().GetName()
		}
	}

	return "?"
}

// openWatch holds a watch open for the test, handing over each batch the stream delivers.
func openWatch(t *testing.T, instance *komradetest.Instance) <-chan client.WatchBatch {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	batches := make(chan client.WatchBatch, 64)
	go client.Watch(ctx, client.WatchParams{
		Open:    client.OpenWatch(instance.Watches),
		OnBatch: func(batch client.WatchBatch) { batches <- batch },
		OnLost: func(err error) {
			if ctx.Err() == nil {
				t.Errorf("the watch stream was lost: %v", err)
			}
		},
	})

	return batches
}

func receiveBatch(t *testing.T, batches <-chan client.WatchBatch) client.WatchBatch {
	t.Helper()

	select {
	case batch := <-batches:
		return batch
	case <-time.After(agreeWithin):
		t.Fatal("the watch delivered nothing")

		return client.WatchBatch{}
	}
}

func applyBatch(world *assemble.World, batch client.WatchBatch) {
	for _, event := range batch {
		world.Apply(event)
	}
}

// agentOn is a live agent in a workflow whose checkout is on a branch of a repository.
func agentOn(t *testing.T, st *storetest.Stores, workflowID, name, repositoryURL, branch string) *agentstore.Agent {
	t.Helper()

	agent := st.SeedAgent(t, agentstore.Agent{Name: name, WorkflowID: workflowID})
	require.NoError(t, st.Agents.SetCheckout(t.Context(), agent.ID, agentstore.SetCheckoutParams{RepositoryURL: &repositoryURL, Branch: &branch}))

	return agent
}

// upsertPullRequest writes one pull request on a branch of the watched repository, merged when
// mergedAt is set, and answers its id.
func upsertPullRequest(t *testing.T, st *storetest.Stores, number int, branch string, mergedAt *time.Time) string {
	t.Helper()

	openedAt := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC).Add(time.Duration(number) * time.Hour)
	state := "open"
	if mergedAt != nil {
		state = "merged"
	}

	written, err := st.PullRequests.Upsert(t.Context(), pullrequeststore.UpsertParams{
		Host:              "github.com",
		Forge:             "github",
		ProviderID:        fmt.Sprintf("PR_%d", number),
		URL:               fmt.Sprintf("%s/pull/%d", watchedRepository, number),
		RepositoryURL:     watchedRepository,
		HeadRepositoryURL: watchedRepository,
		Number:            number,
		Title:             fmt.Sprintf("change %d", number),
		HeadBranch:        branch,
		HeadSHA:           fmt.Sprintf("head%d", number),
		BaseBranch:        "main",
		BaseSHA:           "base",
		State:             state,
		OpenedAt:          openedAt,
		ChangedAt:         openedAt,
		MergedAt:          mergedAt,
	})
	require.NoError(t, err)

	return written.ID
}
