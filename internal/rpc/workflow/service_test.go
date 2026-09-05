package workflow

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"weave/internal/agents"
	"weave/internal/store"
	"weave/internal/storetest"
	"weave/internal/tmux"
	paginationv1 "weave/proto/gen/pagination/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

const missingID = "00000000-0000-0000-0000-000000000000"

// TestCreateWorkflow pins what a workflow is stored as, a blank name falling back to a label
// so the list never draws an empty row.
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

// TestCreateWorkflowWorktree covers the worktree config round trip through the handler,
// and the two ways enabling it is refused: no cwd to work against, and a setup or cleanup
// script the caller named itself, which weave would otherwise silently overwrite.
func TestCreateWorkflowWorktree(t *testing.T) {
	tests := []struct {
		name     string
		req      *workflowv1.CreateWorkflowRequest
		wantCode connect.Code
		check    func(t *testing.T, created *workflowv1.Workflow)
	}{
		{
			name: "enabling it with no cwd is refused",
			req: &workflowv1.CreateWorkflowRequest{
				Name:     "backend",
				Worktree: &workflowv1.WorktreeConfig{Enabled: true},
			},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name: "enabling it writes the config the worktree is cut with",
			req: &workflowv1.CreateWorkflowRequest{
				Name: "backend",
				Cwd:  primaryCheckout(t),
				Worktree: &workflowv1.WorktreeConfig{
					Enabled:      true,
					BaseBranch:   "develop",
					Dir:          "/repo/.worktrees",
					CopyPaths:    []string{".env"},
					SymlinkPaths: []string{"node_modules"},
				},
			},
			check: func(t *testing.T, created *workflowv1.Workflow) {
				assert.True(t, created.GetWorktree().GetEnabled())
				assert.Equal(t, "develop", created.GetWorktree().GetBaseBranch())
				assert.Equal(t, "/repo/.worktrees", created.GetWorktree().GetDir())
				assert.Equal(t, []string{".env"}, created.GetWorktree().GetCopyPaths())
				assert.Equal(t, []string{"node_modules"}, created.GetWorktree().GetSymlinkPaths())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := service(t)

			created, err := svc.CreateWorkflow(t.Context(), connect.NewRequest(tt.req))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)
			tt.check(t, created.Msg.GetWorkflow())
		})
	}
}

// TestUpdateWorkflowWorktreeDirSurvivesAReread pins the worktree directory against being
// dropped on the way in: a field the write path forgets still reads back correctly from the
// response it echoes, and only a second read shows it was never stored.
func TestUpdateWorkflowWorktreeDirSurvivesAReread(t *testing.T) {
	svc, _ := service(t)

	created, err := svc.CreateWorkflow(t.Context(), connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name: "backend",
		Cwd:  primaryCheckout(t),
	}))
	require.NoError(t, err)

	id := created.Msg.GetWorkflow().GetId()
	_, err = svc.UpdateWorkflow(t.Context(), connect.NewRequest(&workflowv1.UpdateWorkflowRequest{
		Id:   id,
		Name: "backend",
		Cwd:  primaryCheckout(t),
		Worktree: &workflowv1.WorktreeConfig{
			Enabled: true,
			Dir:     "/repo/.worktrees",
		},
	}))
	require.NoError(t, err)

	got, err := svc.GetWorkflow(t.Context(), connect.NewRequest(&workflowv1.GetWorkflowRequest{Id: id}))
	require.NoError(t, err)
	assert.Equal(t, "/repo/.worktrees", got.Msg.GetWorkflow().GetWorktree().GetDir())
}

// TestGetWorkflow pins that an id nobody holds is not found rather than an empty workflow.
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

// TestArchiveWorkflow covers the agents inside always going with it, since every agent
// belongs to a workflow and there is nowhere to leave one.
func TestArchiveWorkflow(t *testing.T) {
	tests := []struct {
		name      string
		create    bool
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

			req := connect.NewRequest(&workflowv1.ArchiveWorkflowRequest{Id: id})

			_, err := svc.ArchiveWorkflow(ctx, req)
			if tt.twice {
				require.NoError(t, err)
				_, err = svc.ArchiveWorkflow(ctx, req)
			}

			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)

			_, err = st.LiveAgentByName(ctx, "agent")
			require.ErrorIs(t, err, store.ErrNotFound, "the agent is hidden alongside its workflow")

			all, err := st.Agents.ListAll(ctx)
			require.NoError(t, err)
			require.Len(t, all, 1)
			assert.NotNil(t, all[0].ArchivedAt, "and archived rather than removed")
		})
	}
}

// TestRestoreWorkflow covers the codes a restore answers with, where a workflow that is not
// archived is not found because there is nothing to do to it.
func TestRestoreWorkflow(t *testing.T) {
	tests := []struct {
		name     string
		create   bool
		archive  bool
		wantCode connect.Code
	}{
		{
			name:     "an id nobody holds",
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "a workflow that is not archived",
			create:   true,
			wantCode: connect.CodeNotFound,
		},
		{
			name:    "an archived workflow comes back with its agents",
			create:  true,
			archive: true,
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
			if tt.archive {
				_, err := svc.ArchiveWorkflow(ctx, connect.NewRequest(&workflowv1.ArchiveWorkflowRequest{Id: id}))
				require.NoError(t, err)
			}

			resp, err := svc.RestoreWorkflow(ctx, connect.NewRequest(&workflowv1.RestoreWorkflowRequest{Id: id}))

			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))

				return
			}

			require.NoError(t, err)
			assert.Equal(t, "backend", resp.Msg.GetWorkflow().GetName())

			agent, err := st.LiveAgentByName(ctx, "agent")
			require.NoError(t, err)
			require.NotNil(t, agent, "its agent is listed again")
			assert.Nil(t, agent.ArchivedAt)
		})
	}
}

// TestDeleteWorkflow covers the handler's codes: not found for a missing or a live
// workflow, since only an archived one is deletable this way, and the agents inside an
// archived workflow going with it otherwise.
func TestDeleteWorkflow(t *testing.T) {
	tests := []struct {
		name     string
		create   bool
		archive  bool
		wantCode connect.Code
	}{
		{
			name:     "an id nobody holds",
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "a workflow that is not archived",
			create:   true,
			wantCode: connect.CodeNotFound,
		},
		{
			name:    "an archived workflow is destroyed along with its agents",
			create:  true,
			archive: true,
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
			if tt.archive {
				_, err := svc.ArchiveWorkflow(ctx, connect.NewRequest(&workflowv1.ArchiveWorkflowRequest{Id: id}))
				require.NoError(t, err)
			}

			_, err := svc.DeleteWorkflow(ctx, connect.NewRequest(&workflowv1.DeleteWorkflowRequest{Id: id}))

			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))

				return
			}

			require.NoError(t, err)

			got, err := svc.GetWorkflow(ctx, connect.NewRequest(&workflowv1.GetWorkflowRequest{Id: id}))
			assert.Nil(t, got)
			assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))

			all, err := st.Agents.ListAll(ctx)
			require.NoError(t, err)
			assert.Empty(t, all, "its agent is destroyed too")
		})
	}
}

// TestListWorkflows pins how a listing pages, from an empty store to one holding more than a
// page.
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

// TestListWorkflowsRefusesAMangledToken covers a token that decodes but holds an id no row
// can have, which is the caller's mistake rather than a database failure.
func TestListWorkflowsRefusesAMangledToken(t *testing.T) {
	svc, _ := service(t)

	const token = "eyJpIjoibm90LWFuLWlkIn0="

	_, err := svc.ListWorkflows(t.Context(), connect.NewRequest(&workflowv1.ListWorkflowsRequest{
		Pagination: &paginationv1.Pagination{Cursor: proto.String(token)},
	}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

type idlePanes struct{}

func (idlePanes) GetPanes() (map[string]tmux.PaneStatus, error) { return nil, nil }
func (idlePanes) KillPane(string)                               {}
func (idlePanes) NewWindow(_, _, _ string) (string, error)      { return "", nil }
func (idlePanes) SetPaneOwner(_, _ string) error                { return nil }
func (idlePanes) GetPaneOwner(string) string                    { return "" }
func (idlePanes) HostPath() string                              { return "" }

func service(t *testing.T) (*Service, *storetest.Stores) {
	t.Helper()

	st := storetest.Open(t)
	emptied := agents.NewEmptyWorkflowDeleter(agents.EmptyWorkflowDeleterParams{
		Workflows: st.Workflows,
		Agents:    st.Agents,
		Schedules: st.Schedules,
	})
	deleter := agents.NewLifecycleService(agents.LifecycleServiceParams{
		Tmux:           idlePanes{},
		Agents:         st.Agents,
		Workflows:      st.Workflows,
		EmptyWorkflows: emptied,
		Tx:             st.Pool,
	})
	manager := agents.NewWorkflowService(agents.WorkflowParams{
		Workflows:    st.Workflows,
		Agents:       st.Agents,
		AgentDeleter: deleter,
		Emptied:      emptied,
		Schedules:    st.Schedules,
		Tmux:         idlePanes{},
		Tx:           st.Pool,
	})

	marks := agents.NewMarkService(agents.MarkServiceParams{Marks: st.Marks, Tx: st.Pool})

	return NewService(st.Workflows, manager, marks), st
}

func createWorkflow(t *testing.T, svc *Service, name string) string {
	t.Helper()

	created, err := svc.CreateWorkflow(t.Context(), connect.NewRequest(&workflowv1.CreateWorkflowRequest{Name: name}))
	require.NoError(t, err)

	return created.Msg.GetWorkflow().GetId()
}

func primaryCheckout(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".git"), 0o755))

	return dir
}

// TestListWorkflowsArchived covers the flag that picks which set is listed, and the
// archive time riding out with it, which is how a caller tells an archived workflow from
// a live one.
func TestListWorkflowsArchived(t *testing.T) {
	tests := []struct {
		name      string
		archived  bool
		wantNames []string
	}{
		{
			name:      "the live listing leaves out what was archived",
			wantNames: []string{"live"},
		},
		{
			name:      "the archive listing is the archived rows and only those",
			archived:  true,
			wantNames: []string{"archived"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := service(t)
			ctx := t.Context()

			gone := createWorkflow(t, svc, "archived")
			_, err := svc.ArchiveWorkflow(ctx, connect.NewRequest(&workflowv1.ArchiveWorkflowRequest{Id: gone}))
			require.NoError(t, err)
			createWorkflow(t, svc, "live")

			listed, err := svc.ListWorkflows(ctx, connect.NewRequest(&workflowv1.ListWorkflowsRequest{
				Archived: tt.archived,
			}))
			require.NoError(t, err)

			names := make([]string, 0, len(listed.Msg.GetWorkflows()))
			for _, workflow := range listed.Msg.GetWorkflows() {
				names = append(names, workflow.GetName())
			}
			require.Equal(t, tt.wantNames, names)

			if !tt.archived {
				assert.Empty(t, listed.Msg.GetWorkflows()[0].GetArchivedAt(), "a live workflow reports no archive time")

				return
			}

			stamped := listed.Msg.GetWorkflows()[0].GetArchivedAt()
			require.NotEmpty(t, stamped, "and an archived one reports when it was put away")
			_, err = time.Parse(time.RFC3339, stamped)
			require.NoError(t, err, "as RFC 3339")
		})
	}
}
