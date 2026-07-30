package workflow

import (
	"testing"

	"claude-agents/internal/agents"
	"claude-agents/internal/storetest"
	paginationv1 "claude-agents/proto/gen/pagination/v1"
	workflowv1 "claude-agents/proto/gen/workflow/v1"

	agentstore "claude-agents/internal/store/agent"
	schedulestore "claude-agents/internal/store/schedule"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// missingID is a well-formed uuid no workflow holds, for the not-found rows. A
// malformed one would be refused before the lookup and prove something else.
const missingID = "00000000-0000-0000-0000-000000000000"

func TestCreateWorkflow(t *testing.T) {
	tests := []struct {
		name  string
		req   *workflowv1.CreateWorkflowRequest
		check func(t *testing.T, created *workflowv1.Workflow)
	}{
		{
			name: "a blank name falls back to a label, so the list never renders an empty row",
			req:  &workflowv1.CreateWorkflowRequest{Name: "   "},
			check: func(t *testing.T, created *workflowv1.Workflow) {
				assert.Equal(t, "workflow", created.GetName())
			},
		},
		{
			name: "a name alone is enough",
			req:  &workflowv1.CreateWorkflowRequest{Name: "backend"},
			check: func(t *testing.T, created *workflowv1.Workflow) {
				assert.NotEmpty(t, created.GetId())
				assert.Equal(t, "backend", created.GetName())
			},
		},
		{
			name: "a home-relative directory is expanded before it is stored",
			req: &workflowv1.CreateWorkflowRequest{
				Name:    "platform",
				Context: "shared notes",
				Cwd:     "~/repo",
				Flags:   "--model opus",
			},
			check: func(t *testing.T, created *workflowv1.Workflow) {
				assert.Equal(t, "shared notes", created.GetContext())
				assert.NotEqual(t, "~/repo", created.GetCwd())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := service(t)

			created, err := svc.CreateWorkflow(t.Context(), connect.NewRequest(tt.req))
			require.NoError(t, err)
			tt.check(t, created.Msg.GetWorkflow())
		})
	}
}

func TestGetWorkflow(t *testing.T) {
	tests := []struct {
		name     string
		create   bool
		id       string
		wantCode connect.Code
	}{
		{
			name:     "an id nobody holds",
			id:       missingID,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "a malformed id is refused rather than looked up",
			id:       "not-a-uuid",
			wantCode: connect.CodeNotFound,
		},
		{
			name:   "a workflow that exists",
			create: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := service(t)

			id := tt.id
			if tt.create {
				id = createWorkflow(t, svc, "backend")
			}

			got, err := svc.GetWorkflow(t.Context(), connect.NewRequest(&workflowv1.GetWorkflowRequest{Id: id}))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)
			assert.Equal(t, "backend", got.Msg.GetWorkflow().GetName())
		})
	}
}

// TestUpdateWorkflow covers that an update writes every field, which is what the
// request says it does.
func TestUpdateWorkflow(t *testing.T) {
	tests := []struct {
		name     string
		create   bool
		req      *workflowv1.UpdateWorkflowRequest
		wantCode connect.Code
		wantName string
		wantCwd  string
	}{
		{
			name: "an id nobody holds",
			req: &workflowv1.UpdateWorkflowRequest{
				Id:   missingID,
				Name: "ghost",
			},
			wantCode: connect.CodeNotFound,
		},
		{
			name:   "every field is replaced",
			create: true,
			req: &workflowv1.UpdateWorkflowRequest{
				Name:    "platform",
				Context: "rewritten",
				Cwd:     "/repo",
			},
			wantName: "platform",
			wantCwd:  "/repo",
		},
		{
			name:   "a blank name keeps the one it has",
			create: true,
			req: &workflowv1.UpdateWorkflowRequest{
				Context: "rewritten",
			},
			wantName: "backend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)

			req := tt.req
			if tt.create {
				req.Id = createWorkflow(t, svc, "backend")
			}

			_, err := svc.UpdateWorkflow(t.Context(), connect.NewRequest(req))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)

			stored, err := st.Workflows.Get(t.Context(), req.GetId())
			require.NoError(t, err)
			require.NotNil(t, stored)
			assert.Equal(t, tt.wantName, stored.Name)
			assert.Equal(t, "rewritten", stored.Context)
			if tt.wantCwd != "" {
				assert.Equal(t, tt.wantCwd, stored.Cwd)
			}
		})
	}
}

// TestDeleteWorkflow covers what goes with the workflow and what stops the
// delete. The agents inside always go, since every agent belongs to a workflow
// and there is nowhere to leave one.
func TestDeleteWorkflow(t *testing.T) {
	tests := []struct {
		name   string
		create bool
		// scheduled leaves a live schedule launching into the workflow, which is
		// what refuses the delete.
		scheduled bool
		twice     bool
		wantCode  connect.Code
	}{
		{
			name:     "an id nobody holds",
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "deleting it twice is not a silent success",
			create:   true,
			twice:    true,
			wantCode: connect.CodeNotFound,
		},
		{
			name:   "the agents inside are archived alongside it",
			create: true,
		},
		{
			name:      "a workflow still scheduled into is refused",
			create:    true,
			scheduled: true,
			wantCode:  connect.CodeFailedPrecondition,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)
			ctx := t.Context()

			id := missingID
			if tt.create {
				id = createWorkflow(t, svc, "backend")
				st.SeedAgent(t, agentstore.Agent{Name: "agent", WorkflowID: id})
			}
			if tt.scheduled {
				st.SeedSchedule(t, schedulestore.Config{
					Name: "nightly", Cron: "0 2 * * *", Prompt: "sweep", Enabled: true, WorkflowID: id,
				})
			}

			req := connect.NewRequest(&workflowv1.DeleteWorkflowRequest{Id: id})

			_, err := svc.DeleteWorkflow(ctx, req)
			if tt.twice {
				require.NoError(t, err)
				_, err = svc.DeleteWorkflow(ctx, req)
			}

			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)

			agent, err := st.Agents.Get(ctx, "agent")
			require.NoError(t, err)
			assert.Nil(t, agent, "the agent is hidden alongside its workflow")

			all, err := st.Agents.ListAll(ctx)
			require.NoError(t, err)
			require.Len(t, all, 1)
			assert.True(t, all[0].Deleted, "and archived rather than removed")
		})
	}
}

func TestListWorkflows(t *testing.T) {
	tests := []struct {
		name      string
		seed      []string
		limit     int32
		wantFirst int
		wantMore  bool
	}{
		{
			name:      "an empty store offers no next page",
			wantFirst: 0,
		},
		{
			name:      "a partial page is the last one",
			seed:      []string{"one", "two"},
			limit:     10,
			wantFirst: 2,
		},
		{
			name:      "a full page offers the next",
			seed:      []string{"one", "two", "three"},
			limit:     2,
			wantFirst: 2,
			wantMore:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := service(t)
			ctx := t.Context()

			for _, name := range tt.seed {
				createWorkflow(t, svc, name)
			}

			page := &paginationv1.Pagination{}
			if tt.limit > 0 {
				page.Limit = proto.Int32(tt.limit)
			}

			first, err := svc.ListWorkflows(ctx, connect.NewRequest(&workflowv1.ListWorkflowsRequest{Pagination: page}))
			require.NoError(t, err)
			require.Len(t, first.Msg.GetWorkflows(), tt.wantFirst)

			if !tt.wantMore {
				assert.Empty(t, first.Msg.GetNextPageToken())
				return
			}

			require.NotEmpty(t, first.Msg.GetNextPageToken())

			second, err := svc.ListWorkflows(ctx, connect.NewRequest(&workflowv1.ListWorkflowsRequest{
				Pagination: &paginationv1.Pagination{
					Limit:  proto.Int32(tt.limit),
					Cursor: proto.String(first.Msg.GetNextPageToken()),
				},
			}))
			require.NoError(t, err)

			seen := map[string]bool{}
			for _, w := range append(first.Msg.GetWorkflows(), second.Msg.GetWorkflows()...) {
				assert.False(t, seen[w.GetId()], "a workflow appears on one page only")
				seen[w.GetId()] = true
			}
			assert.Len(t, seen, len(tt.seed))
		})
	}
}

// The handler never sees an agent store, but deleting a workflow settles the
// agents inside it, so those cases reach for one.
func service(t *testing.T) (*Service, *storetest.Stores) {
	t.Helper()

	st := storetest.Open(t)
	manager := agents.NewWorkflowService(agents.WorkflowParams{
		Workflows: st.Workflows,
		Schedules: st.Schedules,
		Tx:        st.Pool,
	})

	return NewService(st.Workflows, manager), st
}

// createWorkflow adds a workflow through the handler and returns its id, since
// most rows here exist only to be acted on.
func createWorkflow(t *testing.T, svc *Service, name string) string {
	t.Helper()

	created, err := svc.CreateWorkflow(t.Context(), connect.NewRequest(&workflowv1.CreateWorkflowRequest{Name: name}))
	require.NoError(t, err)

	return created.Msg.GetWorkflow().GetId()
}
