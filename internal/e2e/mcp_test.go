//go:build integration

package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"weave/internal/caller"
	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
)

// TestIntegrationMCPTools walks the tools an agent's CLI calls, from the list it is served to what
// a call does and how a refusal reads.
func TestIntegrationMCPTools(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})

	workflowID := createWorkflow(t, instance, "checkout")
	api := createAgent(t, instance, workflowID, "api")
	migrations := createAgent(t, instance, workflowID, "migrations")
	orchestrator := startOrchestrator(t, instance, workflowID)

	implementerTools := connectMCP(t, instance, api.GetId())

	t.Run("an implementer is served the tools for writing work", func(t *testing.T) {
		served := implementerTools.listTools(t)

		assert.Subset(t, served, []string{"send_mail", "read_mail", "create_agent", "request_review", "query"})
		assert.NotContains(t, served, "start_orchestrator")
		assert.NotContains(t, served, "rebase_worktree")
	})

	t.Run("an orchestrator is served the tools for handing work out", func(t *testing.T) {
		served := connectMCP(t, instance, orchestrator.GetId()).listTools(t)

		assert.Subset(t, served, []string{"send_mail", "create_agent", "start_orchestrator", "rebase_worktree", "query"})
		assert.NotContains(t, served, "request_review")
	})

	t.Run("a caller weave cannot place is served every tool", func(t *testing.T) {
		served := connectMCP(t, instance, "").listTools(t)

		assert.Subset(t, served, implementerTools.listTools(t))
		assert.Subset(t, served, []string{"start_orchestrator", "rebase_worktree"})
	})

	t.Run("a call is made as the agent calling it", func(t *testing.T) {
		answer, failed := implementerTools.call(t, "send_mail", map[string]any{
			"to":   migrations.GetId(),
			"body": "what is the refund column called?",
		})
		require.False(t, failed, answer)

		var sent agentv1.SendMailResponse
		require.NoError(t, protojson.Unmarshal([]byte(answer), &sent))
		assert.Equal(t, api.GetId(), sent.GetMail().GetFromAgentId())

		read := readMail(t, instance, migrations.GetId())
		require.Len(t, read, 1)
		assert.Equal(t, "what is the refund column called?", read[0].GetBody())
		assert.Equal(t, "api", read[0].GetFromName())
	})

	t.Run("a refusal comes back as a tool error naming its code", func(t *testing.T) {
		answer, failed := implementerTools.call(t, "send_mail", map[string]any{
			"to":   "nobody",
			"body": "hello?",
		})

		assert.True(t, failed)
		assert.Contains(t, answer, "not_found")
	})

	t.Run("a query reads the database", func(t *testing.T) {
		answer, failed := implementerTools.call(t, "query", map[string]any{
			"sql": "SELECT name FROM agents WHERE role = 'implementer' ORDER BY name",
		})
		require.False(t, failed, answer)

		var queried struct {
			Rows []map[string]string `json:"rows"`
		}
		require.NoError(t, json.Unmarshal([]byte(answer), &queried))
		assert.Equal(t, []map[string]string{
			{"name": "api"},
			{"name": "migrations"},
		}, queried.Rows)
	})

	t.Run("a query cannot write", func(t *testing.T) {
		answer, failed := implementerTools.call(t, "query", map[string]any{
			"sql": "DELETE FROM agents",
		})

		assert.True(t, failed, answer)
		assert.ElementsMatch(t, []string{"api", "migrations", "orchestrator"}, liveNames(t, instance))
	})
}

// toolSession is an MCP session held as one caller.
type toolSession struct {
	agentID string
	session *mcpsdk.ClientSession
}

// connectMCP opens a tool session as an agent, or as nobody weave can place when agentID is empty.
func connectMCP(t *testing.T, instance *weavetest.Instance, agentID string) toolSession {
	t.Helper()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "e2e", Version: "0"}, nil)

	session, err := client.Connect(callingAs(t, agentID), &mcpsdk.StreamableClientTransport{
		Endpoint:             instance.URL + "/mcp",
		HTTPClient:           &http.Client{Transport: caller.Transport(http.DefaultTransport)},
		DisableStandaloneSSE: true,
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	return toolSession{agentID: agentID, session: session}
}

func (s toolSession) listTools(t *testing.T) []string {
	t.Helper()

	var names []string
	for tool, err := range s.session.Tools(callingAs(t, s.agentID), nil) {
		require.NoError(t, err)
		names = append(names, tool.Name)
	}

	return names
}

// call runs a tool and returns the text it answered with and whether that text is an error.
func (s toolSession) call(t *testing.T, name string, args map[string]any) (string, bool) {
	t.Helper()

	result, err := s.session.CallTool(callingAs(t, s.agentID), &mcpsdk.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	require.NoError(t, err)
	require.Len(t, result.Content, 1)

	text, ok := result.Content[0].(*mcpsdk.TextContent)
	require.True(t, ok, "the tool answered with %T rather than text", result.Content[0])

	return text.Text, result.IsError
}

func callingAs(t *testing.T, agentID string) context.Context {
	t.Helper()

	return caller.With(t.Context(), caller.Caller{AgentID: agentID})
}
