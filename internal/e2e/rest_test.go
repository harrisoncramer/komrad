package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRESTBesideConnect drives an agent's life over plain HTTP, the way a script with curl
// would, and asserts the Connect client sees the same agent throughout. The two are one
// handler behind a transcoder, so what this pins is that the REST door reaches it: the
// paths and verbs the protos declare, an id read out of the URL, a body decoded into the
// request, and a custom verb for the call that is not one of the four.
func TestRESTBesideConnect(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name: "backend",
		Cwd:  t.TempDir(),
	}))
	require.NoError(t, err)

	created := callREST(t, instance, http.MethodPost, "/v1/agents", fmt.Sprintf(
		`{"prompt":"fix the flaky login test","title":"login","workflowId":%q}`,
		workflow.Msg.GetWorkflow().GetId(),
	))

	id := created["agent"].(map[string]any)["id"].(string)
	require.NotEmpty(t, id)

	t.Run("the connect client sees the agent the post created", func(t *testing.T) {
		got, err := instance.Agents.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: id}))
		require.NoError(t, err)
		assert.Equal(t, "login", got.Msg.GetAgent().GetName())
	})

	t.Run("an id in the path is read as the request field", func(t *testing.T) {
		got := callREST(t, instance, http.MethodGet, "/v1/agents/"+id, "")

		assert.Equal(t, "login", got["agent"].(map[string]any)["name"])
	})

	t.Run("a put fills in both the path variables and the body", func(t *testing.T) {
		callREST(t, instance, http.MethodPut, "/v1/agents/"+id+"/metadata/pull_request", `{"value":"https://example.test/pr/1"}`)

		got, err := instance.Agents.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: id}))
		require.NoError(t, err)

		require.Len(t, got.Msg.GetAgent().GetMetadata(), 1)
		assert.Equal(t, "pull_request", got.Msg.GetAgent().GetMetadata()[0].GetKey())
	})

	t.Run("a custom verb carries the call that is not one of the four", func(t *testing.T) {
		callREST(t, instance, http.MethodPost, "/v1/agents/"+id+":archive", `{}`)

		got, err := instance.Agents.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: id}))
		require.NoError(t, err)
		assert.NotEmpty(t, got.Msg.GetAgent().GetArchivedAt(), "the agent the connect client reads is archived")
	})

	t.Run("a delete removes the agent both doors were reading", func(t *testing.T) {
		callREST(t, instance, http.MethodDelete, "/v1/agents/"+id, "")

		_, err := instance.Agents.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: id}))
		require.Error(t, err)
		assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	})
}

// callREST makes one plain HTTP call against the service and returns the decoded body. It
// carries no Connect header, since the whole point is that a caller which knows nothing
// about Connect is served.
func callREST(t *testing.T, instance *weavetest.Instance, method, path, body string) map[string]any {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	req, err := http.NewRequestWithContext(t.Context(), method, instance.URL+path, reader)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	payload, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "%s %s answered %s", method, path, payload)

	decoded := map[string]any{}
	if len(payload) > 0 {
		require.NoError(t, json.Unmarshal(payload, &decoded))
	}

	return decoded
}
