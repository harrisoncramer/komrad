//go:build integration

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"weave/internal/caller"
	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListMailDirectionOverJSON pins what a JSON caller gets for the direction it names, and the
// refusal a direction weave does not have earns.
func TestListMailDirectionOverJSON(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name: "checkout",
		Cwd:  t.TempDir(),
	}))
	require.NoError(t, err)

	alpha := createAgent(t, instance, workflow.Msg.GetWorkflow().GetId(), "alpha")
	beta := createAgent(t, instance, workflow.Msg.GetWorkflow().GetId(), "beta")

	sendMail(t, instance, alpha.GetId(), beta.GetId(), "what alpha said")
	sendMail(t, instance, beta.GetId(), alpha.GetId(), "what beta said")

	url := instance.URL + agentv1connect.AgentServiceListMailProcedure

	tests := []struct {
		name        string
		contentType string
		request     string
		want        string
	}{
		{
			name:    "the name of a direction reads that half",
			request: `{"id":%q,"direction":"MAIL_DIRECTION_SENT"}`,
			want:    "what alpha said",
		},
		{
			name:    "an omitted direction reads the mailbox",
			request: `{"id":%q}`,
			want:    "what beta said",
		},
		{
			name:    "a direction weave does not have is refused",
			request: `{"id":%q,"direction":"SENT"}`,
		},
		{
			name:    "so is a number no direction is numbered",
			request: `{"id":%q,"direction":99}`,
		},
		{
			name:    "a field weave does not have is refused",
			request: `{"id":%q,"directoin":"MAIL_DIRECTION_SENT"}`,
		},
		{
			name:    "the number of a direction reads that half",
			request: `{"id":%q,"direction":2}`,
			want:    "what alpha said",
		},
		{
			name:    "a number no direction has is refused",
			request: `{"id":%q,"direction":99}`,
		},
		{
			name:        "spelling out the charset reaches the same codec",
			contentType: "application/json; charset=utf-8",
			request:     `{"id":%q,"direction":"SENT"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contentType := tt.contentType
			if contentType == "" {
				contentType = "application/json"
			}

			status, payload := postJSON(t, url, contentType, fmt.Sprintf(tt.request, alpha.GetId()))

			if tt.want == "" {
				assert.Equal(t, http.StatusBadRequest, status, "body: %s", payload)
				return
			}

			require.Equal(t, http.StatusOK, status, "body: %s", payload)

			var listed struct {
				Mail []struct {
					Body string `json:"body"`
				} `json:"mail"`
			}
			require.NoError(t, json.Unmarshal(payload, &listed))
			require.Len(t, listed.Mail, 1)
			assert.Equal(t, tt.want, listed.Mail[0].Body)
		})
	}
}

func sendMail(t *testing.T, instance *weavetest.Instance, fromID, toID, body string) {
	t.Helper()

	req := connect.NewRequest(&agentv1.SendMailRequest{To: toID, Body: body})
	caller.Caller{AgentID: fromID}.SetHeader(req.Header())

	_, err := instance.Agents.SendMail(t.Context(), req)
	require.NoError(t, err)
}

func postJSON(t *testing.T, url, contentType, body string) (int, []byte) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, bytes.NewReader([]byte(body)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	payload, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return resp.StatusCode, payload
}
