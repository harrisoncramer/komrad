package agent

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"weave/internal/interceptors"
	"weave/lib/xptr"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	paginationv1 "weave/proto/gen/pagination/v1"

	agentstore "weave/internal/store/agent"

	"connectrpc.com/connect"
	"github.com/google/uuid"
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
			name:    "a relative directory would be read against whichever pane is current",
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
			// Word matching has no floor of its own, and the trigram fallback covers
			// what is too short for it, so a query this brief is served rather than
			// refused.
			name: "a query of two characters is accepted",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.SearchTranscripts(t.Context(), connect.NewRequest(&agentv1.SearchTranscriptsRequest{Query: "ab"}))

				return err
			},
			wantErr: false,
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
			name: "a get with no id is refused",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.GetAgent(t.Context(), connect.NewRequest(&agentv1.GetAgentRequest{Id: ""}))

				return err
			},
			wantErr: true,
		},
		{
			name: "a delete naming no agent is refused",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.DeleteAgents(t.Context(), connect.NewRequest(&agentv1.DeleteAgentsRequest{Ids: nil}))

				return err
			},
			wantErr: true,
		},
		{
			name: "so is one carrying an empty id among real ones",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.DeleteAgents(t.Context(), connect.NewRequest(&agentv1.DeleteAgentsRequest{
					Ids: []string{uuid.NewString(), ""},
				}))

				return err
			},
			wantErr: true,
		},
		{
			name: "a delete past the ceiling on how many go at once is refused",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				ids := make([]string, 0, 26)
				for range 26 {
					ids = append(ids, uuid.NewString())
				}

				_, err := c.DeleteAgents(t.Context(), connect.NewRequest(&agentv1.DeleteAgentsRequest{Ids: ids}))

				return err
			},
			wantErr: true,
		},
		{
			name: "an archive naming the same agent twice is refused, since it says nothing twice",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				id := uuid.NewString()
				_, err := c.ArchiveAgents(t.Context(), connect.NewRequest(&agentv1.ArchiveAgentsRequest{
					Ids: []string{id, id},
				}))

				return err
			},
			wantErr: true,
		},
		{
			name: "a restore past the ceiling on how many go at once is refused",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				ids := make([]string, 0, 101)
				for range 101 {
					ids = append(ids, uuid.NewString())
				}

				_, err := c.RestoreAgents(t.Context(), connect.NewRequest(&agentv1.RestoreAgentsRequest{Ids: ids}))

				return err
			},
			wantErr: true,
		},
		{
			name: "a mark carrying whitespace is refused, since it is drawn into a row",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.SetMarks(t.Context(), connect.NewRequest(&agentv1.SetMarksRequest{
					Ids:  []string{uuid.NewString()},
					Mark: xptr.Ptr("two glyphs"),
				}))

				return err
			},
			wantErr: true,
		},
		{
			name: "a mark with no agent to put it on is refused",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.SetMarks(t.Context(), connect.NewRequest(&agentv1.SetMarksRequest{
					Ids:  nil,
					Mark: xptr.Ptr("◐"),
				}))

				return err
			},
			wantErr: true,
		},
		{
			name: "the same agent named twice is refused",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				id := uuid.NewString()
				_, err := c.SetMarks(t.Context(), connect.NewRequest(&agentv1.SetMarksRequest{
					Ids:  []string{id, id},
					Mark: xptr.Ptr("◐"),
				}))

				return err
			},
			wantErr: true,
		},
		{
			name: "a glyph is not what the constraint stops",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.SetMarks(t.Context(), connect.NewRequest(&agentv1.SetMarksRequest{
					Ids:  []string{uuid.NewString()},
					Mark: xptr.Ptr("◐"),
				}))

				// The id is nobody's, so the handler answers not_found once the mark
				// itself has been let through.
				if connect.CodeOf(err) == connect.CodeNotFound {
					return nil
				}

				return err
			},
		},
		{
			// The action is written into the log as a structured attribute, the same
			// concern that already guards the message field.
			name: "a log action carrying a control character is refused",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				action := "run\x1b[31mred"
				_, err := c.Log(t.Context(), connect.NewRequest(&agentv1.LogRequest{
					Id:      xptr.Ptr(uuid.NewString()),
					Message: "starting",
					Action:  &action,
				}))

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
		{
			// A key that reads differently agent to agent is not worth querying across
			// them, which is most of why the metadata is keyed at all.
			name: "a metadata key carrying a capital is refused",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.SetMetadata(t.Context(), connect.NewRequest(&agentv1.SetMetadataRequest{
					Id:    uuid.NewString(),
					Key:   "PullRequest",
					Value: xptr.Ptr("https://github.com/o/r/pull/1"),
				}))

				return err
			},
			wantErr: true,
		},
		{
			name: "so is one carrying a space",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.SetMetadata(t.Context(), connect.NewRequest(&agentv1.SetMetadataRequest{
					Id:    uuid.NewString(),
					Key:   "pull request",
					Value: xptr.Ptr("https://github.com/o/r/pull/1"),
				}))

				return err
			},
			wantErr: true,
		},
		{
			name: "a metadata write with no key at all is refused",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.SetMetadata(t.Context(), connect.NewRequest(&agentv1.SetMetadataRequest{
					Id:    uuid.NewString(),
					Key:   "",
					Value: xptr.Ptr("https://github.com/o/r/pull/1"),
				}))

				return err
			},
			wantErr: true,
		},
		{
			// The value is drawn into the panel's frame, so a newline would break the
			// row it lands in rather than fail.
			name: "a metadata value spanning two lines is refused",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.SetMetadata(t.Context(), connect.NewRequest(&agentv1.SetMetadataRequest{
					Id:    uuid.NewString(),
					Key:   "pr",
					Value: xptr.Ptr("https://github.com/o/r/pull/1\nand another"),
				}))

				return err
			},
			wantErr: true,
		},
		{
			name: "a slug key and a single-line url are what the constraint lets through",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.SetMetadata(t.Context(), connect.NewRequest(&agentv1.SetMetadataRequest{
					Id:    uuid.NewString(),
					Key:   "linear_issue",
					Value: xptr.Ptr("https://linear.app/x/issue/ENG-1"),
				}))

				// The id is nobody's, so the handler answers not_found once the key and
				// value have been let through.
				if connect.CodeOf(err) == connect.CodeNotFound {
					return nil
				}

				return err
			},
		},
		{
			name: "clearing a key needs no value",
			call: func(t *testing.T, c agentv1connect.AgentServiceClient) error {
				_, err := c.SetMetadata(t.Context(), connect.NewRequest(&agentv1.SetMetadataRequest{
					Id:  uuid.NewString(),
					Key: "pr",
				}))

				return err
			},
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
	st.SeedAgent(t, agentstore.Agent{Name: "alpha", State: agentstore.StateIdle})

	validation, err := interceptors.NewValidation()
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.Handle(agentv1connect.NewAgentServiceHandler(svc, connect.WithInterceptors(validation)))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return agentv1connect.NewAgentServiceClient(server.Client(), server.URL), launcher
}
