package e2e

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"weave/internal/caller"
	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestJSONIsReadStrictly drives the real HTTP handler, since what is checked is the codec the
// server reads a request with rather than anything a generated client would let a test express.
func TestJSONIsReadStrictly(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name: "checkout",
		Cwd:  t.TempDir(),
	}))
	require.NoError(t, err)

	sender := createAgent(t, instance, workflow.Msg.GetWorkflow().GetId(), "sender")
	recipient := createAgent(t, instance, workflow.Msg.GetWorkflow().GetId(), "recipient")

	send := connect.NewRequest(&agentv1.SendMailRequest{
		ToAgentId: recipient.GetId(),
		Body:      "the refund column is called refunded_at",
	})
	caller.Caller{AgentID: sender.GetId()}.SetHeader(send.Header())

	_, err = instance.Agents.SendMail(ctx, send)
	require.NoError(t, err)

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantIn     string
	}{
		{
			name:       "the enum value the schema names is read",
			body:       `{"id":"` + sender.GetId() + `","direction":"MAIL_DIRECTION_SENT"}`,
			wantStatus: http.StatusOK,
			wantIn:     "refunded_at",
		},
		{
			name:       "a value the enum does not name is refused rather than dropped",
			body:       `{"id":"` + sender.GetId() + `","direction":"SENT"}`,
			wantStatus: http.StatusBadRequest,
			wantIn:     "direction",
		},
		{
			name:       "so is a number the enum does not name",
			body:       `{"id":"` + sender.GetId() + `","direction":99}`,
			wantStatus: http.StatusBadRequest,
			wantIn:     "direction",
		},
		{
			name:       "and a field the message does not have",
			body:       `{"id":"` + sender.GetId() + `","directions":"MAIL_DIRECTION_SENT"}`,
			wantStatus: http.StatusBadRequest,
			wantIn:     "directions",
		},
		{
			name:       "leaving the direction out still asks for the inbox",
			body:       `{"id":"` + recipient.GetId() + `"}`,
			wantStatus: http.StatusOK,
			wantIn:     "refunded_at",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := postJSON(t, instance, "agent.v1.AgentService/ListMail", tt.body)

			assert.Equal(t, tt.wantStatus, status, "body: %s", body)
			assert.Contains(t, body, tt.wantIn)
		})
	}
}

func postJSON(t *testing.T, instance *weavetest.Instance, procedure, body string) (int, string) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		instance.URL+"/"+procedure, bytes.NewReader([]byte(body)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()

	answer, err := io.ReadAll(res.Body)
	require.NoError(t, err)

	return res.StatusCode, string(answer)
}
