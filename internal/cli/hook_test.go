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
		mail     []mailItem
		wantSent bool
		wantText []string
	}{
		{
			name:    "a hook already running is not answered again",
			agentID: "agent-a",
			payload: `{"stop_hook_active":true}`,
			mail: []mailItem{
				{ID: "mail-1", FromName: "api", Body: "which column?"},
			},
		},
		{
			name:    "a caller that is not an agent has no mailbox",
			payload: `{"stop_hook_active":false}`,
			mail:    []mailItem{{ID: "mail-1", Body: "hello"}},
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
			mail: []mailItem{
				{ID: "mail-1", FromAgent: "agent-b", FromName: "migrations", Body: "refunded_cents"},
				{ID: "mail-2", FromAgent: "agent-b", FromName: "migrations", Body: "and it is not null", ReplyTo: "mail-0"},
			},
			wantSent: true,
			wantText: []string{
				"From migrations (agent-b), mail mail-1",
				"refunded_cents",
				"answering mail-0",
				"and it is not null",
				"send_mail",
			},
		},
		{
			name:     "mail from the panel names it as the sender",
			agentID:  "agent-a",
			payload:  `{"stop_hook_active":false}`,
			mail:     []mailItem{{ID: "mail-1", Body: "stand down"}},
			wantSent: true,
			wantText: []string{"From the panel, mail mail-1"},
		},
		{
			name:    "a lifecycle notice from weave is not framed as the user talking",
			agentID: "agent-a",
			payload: `{"stop_hook_active":false}`,
			mail: []mailItem{
				{ID: "mail-1", FromName: "weave", Body: `Your delegate "child" (agent-c) was archived.`},
			},
			wantSent: true,
			wantText: []string{"From weave, mail mail-1", "Weave sent this itself"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var asked caller.Caller
			var logged []string
			addr := fakeMailServer(t, fakeMailParams{mail: tt.mail, asked: &asked, logged: &logged})

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
			assert.Contains(t, logged[0], fmt.Sprintf("delivered %d piece(s) of mail", len(tt.mail)))
			for _, item := range tt.mail {
				assert.Contains(t, logged[0], item.ID)
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

func TestMailPostamble(t *testing.T) {
	tests := []struct {
		name string
		mail []mailItem
		want string
	}{
		{
			name: "weave's own notice, alone",
			mail: []mailItem{{FromName: "weave", Body: "your delegate was deleted"}},
			want: mailFromWeave,
		},
		{
			name: "the panel operator, alone",
			mail: []mailItem{{Body: "stand down"}},
			want: mailFromPerson,
		},
		{
			name: "another agent, alone",
			mail: []mailItem{{FromAgent: "agent-b", FromName: "migrations", Body: "hi"}},
			want: mailFromAgent,
		},
		{
			name: "a person's mail outranks a weave notice in the same batch",
			mail: []mailItem{
				{FromName: "weave", Body: "your delegate was archived"},
				{Body: "stand down"},
			},
			want: mailFromPerson,
		},
		{
			name: "an agent's mail outranks everything else in the same batch",
			mail: []mailItem{
				{FromName: "weave", Body: "your delegate was archived"},
				{Body: "stand down"},
				{FromAgent: "agent-b", FromName: "migrations", Body: "hi"},
			},
			want: mailFromAgent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, mailPostamble(tt.mail))
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
	mail   []mailItem
	asked  *caller.Caller
	logged *[]string
}

func fakeMailServer(t *testing.T, params fakeMailParams) string {
	t.Helper()

	mailPath, err := toolPath(readMailTool)
	require.NoError(t, err)

	logPath, err := toolPath(logTool)
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc(mailPath, func(w http.ResponseWriter, r *http.Request) {
		*params.asked = caller.FromHeader(r.Header)

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(deliveredMail{Mail: params.mail}))
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
