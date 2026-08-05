package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"weave/config"
	"weave/internal/caller"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStopHookDeliversMail(t *testing.T) {
	tests := []struct {
		name     string
		agentID  string
		payload  string
		messages []deliveredMessage
		wantSent bool
		wantText []string
	}{
		{
			name:    "a hook already running is not answered again",
			agentID: "agent-a",
			payload: `{"stop_hook_active":true}`,
			messages: []deliveredMessage{
				{ID: "message-1", FromName: "api", Body: "which column?"},
			},
		},
		{
			name:     "a caller that is not an agent has no mailbox",
			payload:  `{"stop_hook_active":false}`,
			messages: []deliveredMessage{{ID: "message-1", Body: "hello"}},
		},
		{
			name:     "an empty mailbox lets the turn end",
			agentID:  "agent-a",
			payload:  `{"stop_hook_active":false}`,
			wantSent: true,
		},
		{
			name:    "mail is handed back as context",
			agentID: "agent-a",
			payload: `{"stop_hook_active":false}`,
			messages: []deliveredMessage{
				{ID: "message-1", FromAgent: "agent-b", FromName: "migrations", Body: "refunded_cents"},
				{ID: "message-2", FromAgent: "agent-b", FromName: "migrations", Body: "and it is not null", ReplyTo: "message-0"},
			},
			wantSent: true,
			wantText: []string{
				"From migrations (agent-b), message message-1",
				"refunded_cents",
				"answering message-0",
				"and it is not null",
				"send_message",
			},
		},
		{
			name:     "a message from the panel names it as the sender",
			agentID:  "agent-a",
			payload:  `{"stop_hook_active":false}`,
			messages: []deliveredMessage{{ID: "message-1", Body: "stand down"}},
			wantSent: true,
			wantText: []string{"From the panel, message message-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var asked caller.Caller
			var logged []string
			addr := fakeMailServer(t, fakeMailParams{messages: tt.messages, asked: &asked, logged: &logged})

			state := &app{env: &config.Environment{Addr: addr, AgentID: tt.agentID}}

			var out bytes.Buffer
			cmd := stopHookCmd(state)
			cmd.SetIn(bytes.NewBufferString(tt.payload))
			cmd.SetOut(&out)
			cmd.SetArgs(nil)

			require.NoError(t, cmd.Execute())
			assert.Equal(t, tt.wantSent, asked.AgentID != "", "the mailbox is only read when there is one to read")

			if len(tt.wantText) == 0 {
				assert.Empty(t, out.String(), "nothing is printed, so the turn ends")
				assert.Empty(t, logged, "nothing was delivered, so nothing is logged")

				return
			}

			require.Len(t, logged, 1, "a delivery is logged where a person can find it")
			assert.Contains(t, logged[0], fmt.Sprintf("delivered %d message(s)", len(tt.messages)))
			for _, message := range tt.messages {
				assert.Contains(t, logged[0], message.ID)
			}

			var printed stopHookOutput
			require.NoError(t, json.Unmarshal(out.Bytes(), &printed))
			assert.Equal(t, stopHookEvent, printed.HookSpecificOutput.HookEventName)

			for _, want := range tt.wantText {
				assert.Contains(t, printed.HookSpecificOutput.AdditionalContext, want)
			}
		})
	}
}

func TestStopHookSurvivesAServerThatIsNotThere(t *testing.T) {
	state := &app{env: &config.Environment{Addr: "127.0.0.1:1", AgentID: "agent-a"}}

	var out bytes.Buffer
	cmd := stopHookCmd(state)
	cmd.SetIn(bytes.NewBufferString(`{"stop_hook_active":false}`))
	cmd.SetOut(&out)
	cmd.SetArgs(nil)

	var errOut bytes.Buffer
	cmd.SetErr(&errOut)

	require.NoError(t, cmd.Execute())
	assert.Empty(t, out.String(), "the turn is never blocked because weave is down")
	assert.Contains(t, errOut.String(), "weave could not be reached")
}

type fakeMailParams struct {
	messages []deliveredMessage
	asked    *caller.Caller
	logged   *[]string
}

func fakeMailServer(t *testing.T, params fakeMailParams) string {
	t.Helper()

	mailPath, err := toolPath(readMessagesTool)
	require.NoError(t, err)

	logPath, err := toolPath(logTool)
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc(mailPath, func(w http.ResponseWriter, r *http.Request) {
		*params.asked = caller.FromHeader(r.Header)

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(deliveredMail{Messages: params.messages}))
	})

	mux.HandleFunc(logPath, func(w http.ResponseWriter, r *http.Request) {
		var line struct {
			Message string `json:"message"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&line))
		*params.logged = append(*params.logged, line.Message)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return strippedHost(t, server.URL)
}
