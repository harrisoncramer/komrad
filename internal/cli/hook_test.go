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
	"weave/proto/gen/agent/v1/agentv1connect"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStopHookDeliversMail pins what the stop hook writes back to the agent for each shape of
// mailbox, and when it records the agent's turn boundary.
func TestStopHookDeliversMail(t *testing.T) {
	tests := []struct {
		name         string
		agentID      string
		payload      string
		mail         []mailItem
		wantSent     bool
		wantBoundary bool
		wantText     []string
	}{
		{
			name:    "a hook already running is not answered again, but its turn still ended",
			agentID: "agent-a",
			payload: `{"stop_hook_active":true}`,
			mail: []mailItem{
				{
					ID:       "mail-1",
					FromName: "api",
					Body:     "which column?",
				},
			},
			wantBoundary: true,
		},
		{
			name:    "a caller that is not an agent has no mailbox",
			payload: `{"stop_hook_active":false}`,
			mail:    []mailItem{{ID: "mail-1", Body: "hello"}},
		},
		{
			name:         "an empty mailbox lets the turn end",
			agentID:      "agent-a",
			payload:      `{"stop_hook_active":false}`,
			wantSent:     true,
			wantBoundary: true,
		},
		{
			name:    "mail is handed back as context",
			agentID: "agent-a",
			payload: `{"stop_hook_active":false}`,
			mail: []mailItem{
				{
					ID:        "mail-1",
					FromAgent: "agent-b",
					FromName:  "migrations",
					Body:      "refunded_cents",
				},
				{
					ID:        "mail-2",
					FromAgent: "agent-b",
					FromName:  "migrations",
					Body:      "and it is not null",
					ReplyTo:   "mail-0",
				},
			},
			wantSent:     true,
			wantBoundary: true,
			wantText: []string{
				"From migrations (agent-b), mail mail-1",
				"refunded_cents",
				"answering mail-0",
				"and it is not null",
				"send_mail",
			},
		},
		{
			name:         "mail from the panel names it as the sender",
			agentID:      "agent-a",
			payload:      `{"stop_hook_active":false}`,
			mail:         []mailItem{{ID: "mail-1", Body: "stand down"}},
			wantSent:     true,
			wantBoundary: true,
			wantText:     []string{"From the panel, mail mail-1"},
		},
		{
			name:    "a lifecycle notice from weave is not framed as the user talking",
			agentID: "agent-a",
			payload: `{"stop_hook_active":false}`,
			mail: []mailItem{
				{
					ID:       "mail-1",
					FromName: "weave",
					Body:     `Your delegate "child" (agent-c) was archived.`,
				},
			},
			wantSent:     true,
			wantBoundary: true,
			wantText:     []string{"From weave, mail mail-1", "Weave sent this itself"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var asked caller.Caller
			var logged []string
			var boundaries []string
			addr := fakeMailServer(t, fakeMailParams{
				mail:       tt.mail,
				asked:      &asked,
				logged:     &logged,
				boundaries: &boundaries,
			})

			state := &app{env: &config.Environment{Addr: addr, AgentID: tt.agentID}}

			var out bytes.Buffer
			cmd := stopHookCmd(state)
			cmd.SetIn(bytes.NewBufferString(tt.payload))
			cmd.SetOut(&out)
			cmd.SetArgs([]string{})

			require.NoError(t, cmd.Execute())
			assert.Equal(t, tt.wantSent, asked.AgentID != "", "the mailbox is only read when there is one to read")
			assert.Equal(t, tt.wantBoundary, len(boundaries) == 1, "a turn boundary is recorded whenever the turn actually ends")
			if tt.wantBoundary {
				assert.Equal(t, tt.agentID, boundaries[0])
			}

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

// TestMailPostamble pins the wording an agent is handed for each mix of senders, so it knows
// whether it is the panel operator or another agent talking.
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
			mail: []mailItem{{
				FromAgent: "agent-b",
				FromName:  "migrations",
				Body:      "hi",
			}},
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
				{
					FromAgent: "agent-b",
					FromName:  "migrations",
					Body:      "hi",
				},
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

// TestStopHookSurvivesAServerThatIsNotThere pins that the hook exits cleanly when nothing is
// serving, since it runs on every turn and must never block one.
func TestStopHookSurvivesAServerThatIsNotThere(t *testing.T) {
	state := &app{env: &config.Environment{Addr: "127.0.0.1:1", AgentID: "agent-a"}}

	var out bytes.Buffer
	cmd := stopHookCmd(state)
	cmd.SetIn(bytes.NewBufferString(`{"stop_hook_active":false}`))
	cmd.SetOut(&out)
	cmd.SetArgs([]string{})

	var errOut bytes.Buffer
	cmd.SetErr(&errOut)

	require.NoError(t, cmd.Execute())
	assert.Empty(t, out.String(), "the turn is never blocked because weave is down")
	assert.Contains(t, errOut.String(), "weave could not be reached")
}

// TestStopFailureHookRecordsTheBoundary covers the event Claude fires when a turn dies rather
// than finishes, which is the only turn end weave hears in that case.
func TestStopFailureHookRecordsTheBoundary(t *testing.T) {
	tests := []struct {
		name         string
		agentID      string
		payload      string
		wantBoundary bool
		wantLogged   []string
	}{
		{
			name:         "the error kind and the message say why in the agent's log",
			agentID:      "agent-a",
			payload:      `{"error":"server_error","last_assistant_message":"API Error: 529 Overloaded."}`,
			wantBoundary: true,
			wantLogged:   []string{"server_error", "529 Overloaded"},
		},
		{
			name:         "a payload naming no kind still ends the turn",
			agentID:      "agent-a",
			payload:      `{}`,
			wantBoundary: true,
			wantLogged:   []string{"an unnamed error"},
		},
		{
			name:         "a payload that will not parse still ends the turn",
			agentID:      "agent-a",
			payload:      `{`,
			wantBoundary: true,
			wantLogged:   []string{"an unnamed error"},
		},
		{
			name:    "a caller that is not an agent has nothing to stamp",
			payload: `{"error":"rate_limit"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var asked caller.Caller
			var logged []string
			var boundaries []string
			addr := fakeMailServer(t, fakeMailParams{
				asked:      &asked,
				logged:     &logged,
				boundaries: &boundaries,
			})

			state := &app{env: &config.Environment{Addr: addr, AgentID: tt.agentID}}

			var out, errs bytes.Buffer
			cmd := stopFailureHookCmd(state)
			cmd.SetIn(bytes.NewBufferString(tt.payload))
			cmd.SetOut(&out)
			cmd.SetErr(&errs)
			cmd.SetArgs([]string{})

			require.NoError(t, cmd.Execute())
			assert.Equal(t, tt.wantBoundary, len(boundaries) == 1, "a turn that died is still a turn ending")
			assert.Empty(t, out.String(), "a dead turn takes no continuation, so nothing is printed back")
			assert.Empty(t, asked.AgentID, "the mailbox is left alone, since claimed mail would go nowhere")

			if len(tt.wantLogged) == 0 {
				assert.Empty(t, logged)

				return
			}

			require.Len(t, logged, 1)
			for _, want := range tt.wantLogged {
				assert.Contains(t, logged[0], want)
			}
		})
	}
}

type fakeMailParams struct {
	mail       []mailItem
	asked      *caller.Caller
	logged     *[]string
	boundaries *[]string
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
		assert.NoError(t, json.NewEncoder(w).Encode(deliveredMail{Mail: params.mail}))
	})

	mux.HandleFunc(agentv1connect.AgentServiceRecordTurnBoundaryProcedure, func(w http.ResponseWriter, r *http.Request) {
		*params.boundaries = append(*params.boundaries, caller.FromHeader(r.Header).AgentID)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})

	mux.HandleFunc(logPath, func(w http.ResponseWriter, r *http.Request) {
		var line struct {
			Message string `json:"message"`
		}
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&line))
		*params.logged = append(*params.logged, line.Message)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return strippedHost(t, server.URL)
}

// TestTurnEndHookRecordsTheBoundary covers codex's notify program, which is handed one JSON
// argument and fires for more than a turn ending, so the type is what decides.
func TestTurnEndHookRecordsTheBoundary(t *testing.T) {
	tests := []struct {
		name         string
		agentID      string
		args         []string
		wantBoundary bool
	}{
		{
			name:         "a completed turn is recorded",
			agentID:      "agent-a",
			args:         []string{`{"type":"agent-turn-complete","turn-id":"t1"}`},
			wantBoundary: true,
		},
		{
			name:    "anything else codex notifies about is not a turn ending",
			agentID: "agent-a",
			args:    []string{`{"type":"agent-something-else"}`},
		},
		{
			name:    "a payload that will not parse records nothing rather than guessing",
			agentID: "agent-a",
			args:    []string{`{`},
		},
		{
			name:         "a bare call with no payload is taken at its word",
			agentID:      "agent-a",
			wantBoundary: true,
		},
		{
			name: "a caller that is not an agent has nothing to stamp",
			args: []string{`{"type":"agent-turn-complete"}`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var asked caller.Caller
			var logged []string
			var boundaries []string
			addr := fakeMailServer(t, fakeMailParams{
				asked:      &asked,
				logged:     &logged,
				boundaries: &boundaries,
			})

			state := &app{env: &config.Environment{Addr: addr, AgentID: tt.agentID}}

			var out, errs bytes.Buffer
			cmd := turnEndHookCmd(state)
			cmd.SetOut(&out)
			cmd.SetErr(&errs)
			cmd.SetArgs(tt.args)

			require.NoError(t, cmd.Execute())
			assert.Equal(t, tt.wantBoundary, len(boundaries) == 1)
			if tt.wantBoundary {
				assert.Equal(t, tt.agentID, boundaries[0])
			}
			assert.Empty(t, out.String(), "notify reads nothing back, so nothing is printed")
		})
	}
}
