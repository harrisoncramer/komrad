package cli

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"komrad/internal/gittest"
	agentv1 "komrad/proto/gen/agent/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAutoSetupServer struct {
	pages   [][]*workflowv1.Workflow
	created []*workflowv1.CreateWorkflowRequest
	started []*agentv1.StartOrchestratorRequest
}

func (f *fakeAutoSetupServer) ListWorkflows(
	_ context.Context,
	req *connect.Request[workflowv1.ListWorkflowsRequest],
) (*connect.Response[workflowv1.ListWorkflowsResponse], error) {
	page := 0
	if cursor := req.Msg.GetPagination().GetCursor(); cursor != "" {
		parsed, err := strconv.Atoi(cursor)
		if err != nil {
			return nil, err
		}
		page = parsed
	}

	resp := &workflowv1.ListWorkflowsResponse{}
	if page < len(f.pages) {
		resp.Workflows = f.pages[page]
	}
	if page+1 < len(f.pages) {
		next := strconv.Itoa(page + 1)
		resp.NextPageToken = &next
	}

	return connect.NewResponse(resp), nil
}

func (f *fakeAutoSetupServer) CreateWorkflow(
	_ context.Context,
	req *connect.Request[workflowv1.CreateWorkflowRequest],
) (*connect.Response[workflowv1.CreateWorkflowResponse], error) {
	f.created = append(f.created, req.Msg)

	return connect.NewResponse(&workflowv1.CreateWorkflowResponse{Workflow: &workflowv1.Workflow{Id: "wf-new"}}), nil
}

func (f *fakeAutoSetupServer) StartOrchestrator(
	_ context.Context,
	req *connect.Request[agentv1.StartOrchestratorRequest],
) (*connect.Response[agentv1.StartOrchestratorResponse], error) {
	f.started = append(f.started, req.Msg)

	return connect.NewResponse(&agentv1.StartOrchestratorResponse{}), nil
}

// TestAutoSetup covers when opening komrad in a directory sets up a workflow there.
func TestAutoSetup(t *testing.T) {
	checkout := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(checkout, link))

	tests := []struct {
		name    string
		dir     string
		git     autoSetupGit
		pages   [][]*workflowv1.Workflow
		created bool
	}{
		{
			name:    "a checkout with no workflow gets one",
			dir:     "/src/app/internal",
			git:     &gittest.Repo{Root: "/src/app"},
			pages:   [][]*workflowv1.Workflow{{{Cwd: "/src/other"}}},
			created: true,
		},
		{
			name:  "a workflow at the checkout's root is kept",
			dir:   "/src/app/internal",
			git:   &gittest.Repo{Root: "/src/app"},
			pages: [][]*workflowv1.Workflow{{{Cwd: "/src/other"}}, {{Cwd: "/src/app"}}},
		},
		{
			name:  "a workflow at the directory opened in is kept",
			dir:   "/src/app/internal",
			git:   &gittest.Repo{Root: "/src/app"},
			pages: [][]*workflowv1.Workflow{{{Cwd: "/src/app/internal/"}}},
		},
		{
			name:  "a workflow made through a symlink to the checkout is kept",
			dir:   checkout,
			git:   &gittest.Repo{Root: checkout},
			pages: [][]*workflowv1.Workflow{{{Cwd: link}}},
		},
		{
			name: "a directory outside git is left alone",
			dir:  "/home/me",
			git:  gittest.NoRepo{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := &fakeAutoSetupServer{pages: tt.pages}

			require.NoError(t, autoSetup(t.Context(), autoSetupParams{
				Dir:       tt.dir,
				Git:       tt.git,
				Workflows: server,
				Agents:    server,
			}))

			if !tt.created {
				assert.Empty(t, server.created)
				assert.Empty(t, server.started)

				return
			}

			require.Len(t, server.created, 1)
			assert.Equal(t, "app", server.created[0].GetName())
			assert.Equal(t, "/src/app", server.created[0].GetCwd())
			assert.True(t, server.created[0].GetWorktree().GetEnabled())

			require.Len(t, server.started, 1)
			assert.Equal(t, "wf-new", server.started[0].GetWorkflowId())
			assert.Equal(t, autoSetupPrompt, server.started[0].GetPrompt())
		})
	}
}
