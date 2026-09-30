//go:build integration

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"komrad/config"
	"komrad/internal/caller"
	"komrad/internal/harness"
	"komrad/internal/harness/claude"
	"komrad/internal/harness/codex"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
)

// TestIntegrationStopHookDeliversMail pins what the stop hook writes back to the agent for each shape of
// mailbox, and when it records the agent's turn boundary.
func TestIntegrationStopHookDeliversMail(t *testing.T) {
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
			name:    "a lifecycle notice from komrad is not framed as the user talking",
			agentID: "agent-a",
			payload: `{"stop_hook_active":false}`,
			mail: []mailItem{
				{
					ID:       "mail-1",
					FromName: "komrad",
					Body:     `Your delegate "child" (agent-c) was archived.`,
				},
			},
			wantSent:     true,
			wantBoundary: true,
			wantText:     []string{"From komrad, mail mail-1", "Komrad sent this itself"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var asked caller.Caller
			var logged []string
			var reports []recordedReport
			addr := fakeMailServer(t, fakeMailParams{
				mail:    tt.mail,
				asked:   &asked,
				logged:  &logged,
				reports: &reports,
			})

			state := &app{
				env:       &config.Environment{Addr: addr, AgentID: tt.agentID},
				harnesses: hookHarnesses(t),
			}

			var out bytes.Buffer
			cmd := stopHookCmd(state)
			cmd.SetIn(bytes.NewBufferString(tt.payload))
			cmd.SetOut(&out)
			cmd.SetArgs([]string{})

			require.NoError(t, cmd.Execute())
			assert.Equal(t, tt.wantSent, asked.AgentID != "", "the mailbox is only read when there is one to read")
			assert.Equal(t, tt.wantBoundary, len(reports) == 1, "a turn boundary is recorded whenever the turn actually ends")
			if tt.wantBoundary {
				assert.Equal(t, recordedReport{agentID: tt.agentID, turnEnded: true}, reports[0],
					"a turn ending is all Stop reports, and it names nothing the agent is doing")
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

			var printed hookOutput
			require.NoError(t, json.Unmarshal(out.Bytes(), &printed))
			assert.Equal(t, string(claude.EventStop), printed.HookSpecificOutput.HookEventName)

			for _, want := range tt.wantText {
				assert.Contains(t, printed.HookSpecificOutput.AdditionalContext, want)
			}
		})
	}
}

// TestIntegrationClampReason covers what the hook does with a reason the endpoint would refuse, which it
// takes off rather than lose the turn end travelling with it.
func TestIntegrationClampReason(t *testing.T) {
	tests := []struct {
		name   string
		reason string
		want   string
	}{
		{name: "nothing to say"},
		{
			name:   "an ordinary tool name",
			reason: "Bash",
			want:   "Bash",
		},
		{
			name:   "a reason carrying what the column cannot hold",
			reason: "Bash\x00 -c\n'ls'",
			want:   "Bash -c'ls'",
		},
		{
			name:   "a reason longer than the field takes",
			reason: strings.Repeat("é", reasonLimit+10),
			want:   strings.Repeat("é", reasonLimit),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clamped := clampReason(tt.reason)
			assert.Equal(t, tt.want, clamped)
			assert.True(t, utf8.ValidString(clamped), "a cut that split a rune would be refused as well")
		})
	}
}

// TestIntegrationEveryInstalledHookHasASubcommand pins that each event komrad points a harness at is
// answered by a subcommand of komrad hook.
func TestIntegrationEveryInstalledHookHasASubcommand(t *testing.T) {
	names := xslice.Map(hookCmd(&app{}).Commands(), func(cmd *cobra.Command) string {
		return cmd.Name()
	})

	for event, install := range claude.InstalledHooks {
		assert.Contains(t, names, install.Subcommand, "the %s hook runs komrad hook %s", event, install.Subcommand)
	}
}

// TestIntegrationMailPostamble pins the wording an agent is handed for each mix of senders, so it knows
// whether it is the panel operator or another agent talking.
func TestIntegrationMailPostamble(t *testing.T) {
	tests := []struct {
		name string
		mail []mailItem
		want string
	}{
		{
			name: "komrad's own notice, alone",
			mail: []mailItem{{FromName: "komrad", Body: "your delegate was deleted"}},
			want: mailFromKomrad,
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
			name: "a person's mail outranks a komrad notice in the same batch",
			mail: []mailItem{
				{FromName: "komrad", Body: "your delegate was archived"},
				{Body: "stand down"},
			},
			want: mailFromPerson,
		},
		{
			name: "an agent's mail outranks everything else in the same batch",
			mail: []mailItem{
				{FromName: "komrad", Body: "your delegate was archived"},
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

// TestIntegrationStopHookSurvivesAServerThatIsNotThere pins that the hook exits cleanly when nothing is
// serving.
func TestIntegrationStopHookSurvivesAServerThatIsNotThere(t *testing.T) {
	state := &app{
		env:       &config.Environment{Addr: "127.0.0.1:1", AgentID: "agent-a"},
		harnesses: hookHarnesses(t),
	}

	var out bytes.Buffer
	cmd := stopHookCmd(state)
	cmd.SetIn(bytes.NewBufferString(`{"stop_hook_active":false}`))
	cmd.SetOut(&out)
	cmd.SetArgs([]string{})

	var errOut bytes.Buffer
	cmd.SetErr(&errOut)

	require.NoError(t, cmd.Execute())
	assert.Empty(t, out.String(), "the turn is never blocked because komrad is down")
	assert.Contains(t, errOut.String(), "komrad could not be reached")
}

// recordedReport is one call of the endpoint every hook event reports through.
type recordedReport struct {
	agentID   string
	reason    string
	turnEnded bool
	turnError string
}

type fakeMailParams struct {
	mail    []mailItem
	asked   *caller.Caller
	logged  *[]string
	reports *[]recordedReport
}

// hookHarnesses is the registry a hook subcommand reads its own payload through.
func hookHarnesses(t *testing.T) *harness.Registry {
	t.Helper()

	registry, err := harness.NewRegistry(
		claude.NewHarness(claude.NewHarnessParams{ConfigDir: t.TempDir()}),
		codex.NewHarness(codex.NewHarnessParams{Home: t.TempDir()}),
	)
	require.NoError(t, err)

	return registry
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
		resp := &agentv1.ReadMailResponse{Mail: xslice.Map(params.mail, func(item mailItem) *agentv1.Mail {
			m := &agentv1.Mail{Id: item.ID, FromName: item.FromName, Body: item.Body}
			if item.FromAgent != "" {
				m.FromAgentId = &item.FromAgent
			}
			if item.ReplyTo != "" {
				m.ReplyTo = &item.ReplyTo
			}

			return m
		})}
		out, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(resp)
		assert.NoError(t, err)
		_, err = w.Write(out)
		assert.NoError(t, err)
	})

	mux.HandleFunc(agentv1connect.AgentServiceRecordReportProcedure, func(w http.ResponseWriter, r *http.Request) {
		var report struct {
			Reason    string `json:"reason"`
			TurnEnded bool   `json:"turnEnded"`
			TurnError string `json:"turnError"`
		}
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&report))
		*params.reports = append(*params.reports, recordedReport{
			agentID:   caller.FromHeader(r.Header).AgentID,
			reason:    report.Reason,
			turnEnded: report.TurnEnded,
			turnError: report.TurnError,
		})

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

// TestIntegrationTurnEndHookRecordsTheBoundary covers codex's notify program.
func TestIntegrationTurnEndHookRecordsTheBoundary(t *testing.T) {
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
			var reports []recordedReport
			addr := fakeMailServer(t, fakeMailParams{
				asked:   &asked,
				logged:  &logged,
				reports: &reports,
			})

			state := &app{
				env:       &config.Environment{Addr: addr, AgentID: tt.agentID},
				harnesses: hookHarnesses(t),
			}

			var out, errs bytes.Buffer
			cmd := turnEndHookCmd(state)
			cmd.SetOut(&out)
			cmd.SetErr(&errs)
			cmd.SetArgs(tt.args)

			require.NoError(t, cmd.Execute())
			assert.Equal(t, tt.wantBoundary, len(reports) == 1)
			if tt.wantBoundary {
				assert.Equal(t, recordedReport{agentID: tt.agentID, turnEnded: true}, reports[0])
			}
			assert.Empty(t, out.String(), "notify reads nothing back, so nothing is printed")
		})
	}
}

// TestIntegrationStopFailureHookRecordsTheFailure covers the hook Claude runs in place of Stop
// when an API error ends the turn.
func TestIntegrationStopFailureHookRecordsTheFailure(t *testing.T) {
	var asked caller.Caller
	var logged []string
	var reports []recordedReport
	addr := fakeMailServer(t, fakeMailParams{
		asked:   &asked,
		logged:  &logged,
		reports: &reports,
	})

	state := &app{
		env:       &config.Environment{Addr: addr, AgentID: "agent-a"},
		harnesses: hookHarnesses(t),
	}

	var out, errs bytes.Buffer
	cmd := hookCmd(state)
	cmd.SetOut(&out)
	cmd.SetErr(&errs)
	cmd.SetIn(strings.NewReader(`{"error":"rate_limit","last_assistant_message":"API Error: Rate limit reached"}`))
	cmd.SetArgs([]string{"stop-failure"})

	require.NoError(t, cmd.Execute())
	require.Len(t, reports, 1)
	assert.Equal(t, recordedReport{agentID: "agent-a", turnEnded: true, turnError: "Rate limit reached"}, reports[0])
	assert.Empty(t, out.String(), "Claude ignores what a StopFailure hook prints")
}
