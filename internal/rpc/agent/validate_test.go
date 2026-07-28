package agent

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"claude-agents/internal/interceptors"
	agentv1 "claude-agents/proto/gen/agent/v1"
	"claude-agents/proto/gen/agent/v1/agentv1connect"
	paginationv1 "claude-agents/proto/gen/pagination/v1"

	agentstore "claude-agents/internal/store/agent"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestCreateAgentValidation(t *testing.T) {
	tests := []struct {
		name    string
		req     *agentv1.CreateAgentRequest
		wantErr bool
	}{
		{
			name: "a prompt, a workflow, and an absolute directory are enough",
			req:  &agentv1.CreateAgentRequest{Prompt: "audit the migrations", Dir: "/repo", WorkflowId: "wf-1"},
		},
		{
			name: "a home-relative directory is allowed, since the manager expands it",
			req:  &agentv1.CreateAgentRequest{Prompt: "go", Dir: "~/repo", WorkflowId: "wf-1"},
		},
		{
			// The workflow supplies the directory, which is what a caller with no
			// view of the filesystem relies on.
			name: "no directory is allowed, since the workflow has one",
			req:  &agentv1.CreateAgentRequest{Prompt: "go", WorkflowId: "wf-1"},
		},
		{
			name:    "no prompt",
			req:     &agentv1.CreateAgentRequest{Dir: "/repo", WorkflowId: "wf-1"},
			wantErr: true,
		},
		{
			name:    "no workflow, which every agent needs",
			req:     &agentv1.CreateAgentRequest{Prompt: "go", Dir: "/repo"},
			wantErr: true,
		},
		{
			name:    "a relative directory would resolve against whichever pane is current",
			req:     &agentv1.CreateAgentRequest{Prompt: "go", Dir: "repo", WorkflowId: "wf-1"},
			wantErr: true,
		},
		{
			name:    "flags carrying a shell metacharacter are refused rather than quoted and run",
			req:     &agentv1.CreateAgentRequest{Prompt: "go", Dir: "/repo", WorkflowId: "wf-1", Flags: "--model opus; rm -rf /"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, launcher := validatedClient(t)

			_, err := client.CreateAgent(t.Context(), connect.NewRequest(tt.req))
			if tt.wantErr {
				assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
				assert.Empty(t, launcher.got.Prompt, "a rejected request should never reach the launcher")
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.req.GetPrompt(), launcher.got.Prompt)
		})
	}
}

// TestRequestValidation covers the constraints on the rest of the surface. They
// live in the proto and run in the interceptor, so each case calls through a
// real client rather than the service directly.
func TestRequestValidation(t *testing.T) {
	tests := []struct {
		name string
		// call is the request as a client would send it. Every case aims at "alpha",
		// the agent validatedClient seeds, so a rejection is the constraint talking
		// and not a missing row.
		call func(t *testing.T, c agentv1connect.AgentServiceClient) error
		// wantErr says the request should be refused as invalid rather than served.
		wantErr bool
	}{
		{
			name: "a known state is accepted",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.SetState(t.Context(), connect.NewRequest(&agentv1.SetStateRequest{Name: "alpha", State: agentstore.StateWaiting}))

				return err
			},
		},
		{
			name: "so is the state a finished agent settles on",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.SetState(t.Context(), connect.NewRequest(&agentv1.SetStateRequest{Name: "alpha", State: agentstore.StateDone}))

				return err
			},
		},
		{
			// An unknown state would store cleanly and then render nowhere, so the
			// set is closed at the edge.
			name: "a state outside the set is refused",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.SetState(t.Context(), connect.NewRequest(&agentv1.SetStateRequest{Name: "alpha", State: "pondering"}))

				return err
			},
			wantErr: true,
		},
		{
			name: "a state change with no name matches no agent",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.SetState(t.Context(), connect.NewRequest(&agentv1.SetStateRequest{Name: "", State: agentstore.StateDone}))

				return err
			},
			wantErr: true,
		},
		{
			// The store would rewrite a name like this rather than store it, and the
			// caller would be told it succeeded under a name it did not ask for.
			name: "a rename to a name with a space is refused",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.UpdateAgent(t.Context(), connect.NewRequest(&agentv1.UpdateAgentRequest{Id: "id-1", Name: "my agent"}))

				return err
			},
			wantErr: true,
		},
		{
			name: "so is one carrying a colon, which tmux reads as a target separator",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.UpdateAgent(t.Context(), connect.NewRequest(&agentv1.UpdateAgentRequest{Id: "id-1", Name: "a:b"}))

				return err
			},
			wantErr: true,
		},
		{
			name: "a rename needs an id to key on",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.UpdateAgent(t.Context(), connect.NewRequest(&agentv1.UpdateAgentRequest{Id: "", Name: "fine"}))

				return err
			},
			wantErr: true,
		},
		{
			name: "an empty search query matches everything",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.SearchTranscripts(t.Context(), connect.NewRequest(&agentv1.SearchTranscriptsRequest{Query: ""}))

				return err
			},
			wantErr: true,
		},
		{
			name: "the trigram index cannot match under three characters",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.SearchTranscripts(t.Context(), connect.NewRequest(&agentv1.SearchTranscriptsRequest{Query: "ab"}))

				return err
			},
			wantErr: true,
		},
		{
			// A limit past the ceiling is clamped rather than refused, so a caller
			// asking for more than the server will serve still gets a page.
			name: "an oversized limit is clamped, not refused",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.SearchTranscripts(t.Context(), connect.NewRequest(&agentv1.SearchTranscriptsRequest{
					Query:      "mcp",
					Pagination: &paginationv1.Pagination{Limit: proto.Int32(5000)},
				}))

				return err
			},
		},
		{
			name: "an unset limit still means the server default",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.SearchTranscripts(t.Context(), connect.NewRequest(&agentv1.SearchTranscriptsRequest{Query: "mcp"}))

				return err
			},
		},
		{
			name: "a get with no name is refused",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.GetAgent(t.Context(), connect.NewRequest(&agentv1.GetAgentRequest{Name: ""}))

				return err
			},
			wantErr: true,
		},
		{
			name: "a delete with no id is refused",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.DeleteAgent(t.Context(), connect.NewRequest(&agentv1.DeleteAgentRequest{Id: ""}))

				return err
			},
			wantErr: true,
		},
		{
			name: "a transcript read with no agent id is refused",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.GetTranscript(t.Context(), connect.NewRequest(&agentv1.GetTranscriptRequest{AgentId: ""}))

				return err
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, _ := validatedClient(t)

			err := tt.call(t, client)
			if tt.wantErr {
				assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
				return
			}

			assert.NoError(t, err)
		})
	}
}

// validatedClient serves the agent service behind the validation interceptor and
// returns a client for it, since the constraints only run on the wire path.
func validatedClient(t *testing.T) (agentv1connect.AgentServiceClient, *fakeLauncher) {
	t.Helper()

	svc, st, launcher := serviceWithLauncher(t)

	// The mutating methods report not_found for an agent that does not exist, so
	// the validation cases need a real one to aim at.
	st.SeedAgent(t, agentstore.Agent{Name: "alpha", State: agentstore.StateNew})

	validation, err := interceptors.NewValidation()
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.Handle(agentv1connect.NewAgentServiceHandler(svc, connect.WithInterceptors(validation)))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return agentv1connect.NewAgentServiceClient(server.Client(), server.URL), launcher
}
