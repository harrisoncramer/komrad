package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	annotationv1 "weave/proto/gen/annotation/v1"

	"weave/internal/caller"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stampAgent names an agent on every request the way a launched CLI's headers do.
type stampAgent struct {
	agentID string
}

func (s stampAgent) RoundTrip(request *http.Request) (*http.Response, error) {
	request.Header.Set(caller.AgentHeader, s.agentID)

	return http.DefaultTransport.RoundTrip(request)
}

// listToolsAs is the tool names the server hands a caller stamped with the agent id.
func listToolsAs(t *testing.T, endpoint, agentID string) map[string]bool {
	t.Helper()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "0.0.1"}, nil)
	session, err := client.Connect(t.Context(), &mcpsdk.StreamableClientTransport{
		Endpoint:   endpoint,
		HTTPClient: &http.Client{Transport: stampAgent{agentID: agentID}},
	}, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	listed, err := session.ListTools(t.Context(), &mcpsdk.ListToolsParams{})
	require.NoError(t, err)

	names := map[string]bool{}
	for _, tool := range listed.Tools {
		names[tool.Name] = true
	}

	return names
}

// TestHandlerServesTheCallersRole checks the whole path a launched agent takes: its id on a
// header, its role read from that, and the tools for that role and no others.
func TestHandlerServesTheCallersRole(t *testing.T) {
	roles := map[string]annotationv1.Role{
		"reviewer":    annotationv1.Role_ROLE_REVIEWER,
		"implementer": annotationv1.Role_ROLE_IMPLEMENTER,
	}

	handler, err := NewHandler(NewHandlerParams{
		BaseURL: "http://127.0.0.1:1",
		RoleOf: func(_ context.Context, agentID string) annotationv1.Role {
			return roles[agentID]
		},
	})
	require.NoError(t, err)

	server := httptest.NewServer(handler)
	defer server.Close()

	reviewer := listToolsAs(t, server.URL, "reviewer")
	assert.True(t, reviewer["create_review_thread"])
	assert.False(t, reviewer["request_review"])

	implementer := listToolsAs(t, server.URL, "implementer")
	assert.True(t, implementer["request_review"])
	assert.False(t, implementer["create_review_thread"])

	// A caller naming an agent weave cannot place keeps the whole surface.
	stranger := listToolsAs(t, server.URL, "stranger")
	assert.True(t, stranger["request_review"])
	assert.True(t, stranger["create_review_thread"])

	// The database tools are nobody's to lose.
	for _, tools := range []map[string]bool{reviewer, implementer, stranger} {
		assert.True(t, tools["query"])
	}
}
