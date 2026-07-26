package session

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"claude-agents/internal/interceptors"
	"claude-agents/internal/store"
	paginationv1 "claude-agents/proto/gen/pagination/v1"
	sessionv1 "claude-agents/proto/gen/session/v1"
	"claude-agents/proto/gen/session/v1/sessionv1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// validatedClient serves the session service behind the validation interceptor
// and returns a client for it. The constraints live in the proto and run in the
// interceptor, so a request has to travel the real path to be checked.
func validatedClient(t *testing.T) (sessionv1connect.SessionServiceClient, *fakeLauncher) {
	t.Helper()

	svc, st, launcher := serviceWithLauncher(t)

	// The mutating methods now report not_found for a chat that does not exist,
	// so the validation cases need a real one to aim at.
	require.NoError(t, st.Upsert(t.Context(), store.Agent{Name: "alpha", State: store.StateNew}))

	validation, err := interceptors.NewValidation()
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.Handle(sessionv1connect.NewSessionServiceHandler(svc, connect.WithInterceptors(validation)))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return sessionv1connect.NewSessionServiceClient(server.Client(), server.URL), launcher
}

func TestCreateSessionValidation(t *testing.T) {
	tests := []struct {
		name    string
		req     *sessionv1.CreateSessionRequest
		wantErr bool
	}{
		{
			name: "a prompt and an absolute directory are enough",
			req:  &sessionv1.CreateSessionRequest{Prompt: "audit the migrations", Dir: "/repo"},
		},
		{
			name: "a home-relative directory is allowed, since the manager expands it",
			req:  &sessionv1.CreateSessionRequest{Prompt: "go", Dir: "~/repo"},
		},
		{
			name:    "no prompt",
			req:     &sessionv1.CreateSessionRequest{Dir: "/repo"},
			wantErr: true,
		},
		{
			name:    "no directory",
			req:     &sessionv1.CreateSessionRequest{Prompt: "go"},
			wantErr: true,
		},
		{
			name:    "a relative directory would resolve against whichever pane is current",
			req:     &sessionv1.CreateSessionRequest{Prompt: "go", Dir: "repo"},
			wantErr: true,
		},
		{
			name:    "an empty workflow id names a workflow that cannot exist",
			req:     &sessionv1.CreateSessionRequest{Prompt: "go", Dir: "/repo", WorkflowId: ptr("")},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, launcher := validatedClient(t)

			_, err := client.CreateSession(t.Context(), connect.NewRequest(tt.req))
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

// TestSetStateValidation confirms the state is a closed set. An unknown value
// would store cleanly and then render nowhere, so it is refused at the edge.
func TestSetStateValidation(t *testing.T) {
	client, _ := validatedClient(t)

	for _, state := range []string{store.StateNew, store.StateThinking, store.StateWaiting, store.StateDone} {
		_, err := client.SetState(t.Context(), connect.NewRequest(&sessionv1.SetStateRequest{Name: "alpha", State: state}))
		assert.NoError(t, err, "state %q should be accepted", state)
	}

	_, err := client.SetState(t.Context(), connect.NewRequest(&sessionv1.SetStateRequest{Name: "alpha", State: "pondering"}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	_, err = client.SetState(t.Context(), connect.NewRequest(&sessionv1.SetStateRequest{Name: "", State: store.StateDone}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "an empty name matches no chat")
}

// TestUpdateSessionValidation confirms a rename is refused when it carries
// characters the store would rewrite, rather than the caller being told it
// succeeded under a different name.
func TestUpdateSessionValidation(t *testing.T) {
	client, _ := validatedClient(t)

	for _, name := range []string{"my agent", "v1.0", "a:b"} {
		_, err := client.UpdateSession(t.Context(), connect.NewRequest(&sessionv1.UpdateSessionRequest{Id: "id-1", Name: name}))
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "name %q should be refused", name)
	}

	_, err := client.UpdateSession(t.Context(), connect.NewRequest(&sessionv1.UpdateSessionRequest{Id: "", Name: "fine"}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "a rename needs an id to key on")
}

func TestSearchTranscriptsValidation(t *testing.T) {
	client, _ := validatedClient(t)

	_, err := client.SearchTranscripts(t.Context(), connect.NewRequest(&sessionv1.SearchTranscriptsRequest{Query: ""}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "an empty query matches everything")

	_, err = client.SearchTranscripts(t.Context(), connect.NewRequest(&sessionv1.SearchTranscriptsRequest{Query: "ab"}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "the trigram index cannot match under three characters")

	// A limit past the ceiling is clamped rather than refused, so a caller asking
	// for more than the server will serve still gets a page.
	_, err = client.SearchTranscripts(t.Context(), connect.NewRequest(&sessionv1.SearchTranscriptsRequest{
		Query:      "mcp",
		Pagination: &paginationv1.Pagination{Limit: proto.Int32(5000)},
	}))
	assert.NoError(t, err, "an oversized limit is clamped")

	_, err = client.SearchTranscripts(t.Context(), connect.NewRequest(&sessionv1.SearchTranscriptsRequest{Query: "mcp"}))
	assert.NoError(t, err, "an unset limit still means the server default")
}

func TestGetAndDeleteValidation(t *testing.T) {
	client, _ := validatedClient(t)

	_, err := client.GetSession(t.Context(), connect.NewRequest(&sessionv1.GetSessionRequest{Name: ""}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	_, err = client.DeleteSession(t.Context(), connect.NewRequest(&sessionv1.DeleteSessionRequest{Name: ""}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	_, err = client.GetTranscript(t.Context(), connect.NewRequest(&sessionv1.GetTranscriptRequest{SessionId: ""}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

func ptr(s string) *string { return &s }
