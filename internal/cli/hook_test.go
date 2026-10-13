//go:build integration

package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"komrad/config"
	"komrad/internal/caller"
	"komrad/internal/harness/claude"
	"komrad/lib/xslice"
	"komrad/proto/gen/agent/v1/agentv1connect"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationStopHookRecordsTheBoundary covers the hook Claude runs as a turn ends.
func TestIntegrationStopHookRecordsTheBoundary(t *testing.T) {
	tests := []struct {
		name        string
		agentID     string
		wantReports []recordedReport
	}{
		{
			name: "a caller that is not an agent has nothing to stamp",
		},
		{
			name:        "a turn ending is recorded",
			agentID:     "agent-a",
			wantReports: []recordedReport{{agentID: "agent-a", event: "REPORT_EVENT_TURN_ENDED", hook: "Stop"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logged []string
			var reports []recordedReport
			addr := fakeHookServer(t, fakeHookServerParams{
				logged:  &logged,
				reports: &reports,
			})

			state := &app{
				env:       &config.Environment{Addr: addr, AgentID: tt.agentID},
				harnesses: hookHarnesses(t),
			}

			var out bytes.Buffer
			cmd := hookCmd(state)
			cmd.SetIn(bytes.NewBufferString(`{"stop_hook_active":false}`))
			cmd.SetOut(&out)
			cmd.SetArgs([]string{"stop"})

			require.NoError(t, cmd.Execute())
			assert.Equal(t, tt.wantReports, reports)
			assert.Empty(t, out.String(), "nothing is handed back, so the turn ends")
		})
	}
}

// TestIntegrationClampPrintable covers what the hook does with a reason the endpoint would refuse, which it
// takes off rather than lose the turn end travelling with it.
func TestIntegrationClampPrintable(t *testing.T) {
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
			reason: strings.Repeat("é", reportLimit+10),
			want:   strings.Repeat("é", reportLimit),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clamped := clampPrintable(tt.reason, reportLimit)
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

// TestIntegrationHookSurvivesAServerThatIsNotThere pins that each hook exits cleanly when nothing is
// serving and says on stderr what it could not do.
func TestIntegrationHookSurvivesAServerThatIsNotThere(t *testing.T) {
	tests := []struct {
		name    string
		hook    string
		payload string
		want    []string
	}{
		{
			name:    "the stop hook",
			hook:    "stop",
			payload: `{"stop_hook_active":false}`,
			want:    []string{"komrad did not record the hook"},
		},
		{
			name:    "a tool call",
			hook:    "pre-tool-use",
			payload: `{"tool_name":"Bash"}`,
			want:    []string{"komrad did not record the hook"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &app{
				env:       &config.Environment{Addr: "127.0.0.1:1", AgentID: "agent-a"},
				harnesses: hookHarnesses(t),
			}

			var out, errOut bytes.Buffer
			cmd := hookCmd(state)
			cmd.SetIn(bytes.NewBufferString(tt.payload))
			cmd.SetOut(&out)
			cmd.SetErr(&errOut)
			cmd.SetArgs([]string{tt.hook})

			require.NoError(t, cmd.Execute())
			assert.Empty(t, out.String(), "the agent is never blocked because komrad is down")
			for _, want := range tt.want {
				assert.Contains(t, errOut.String(), want)
			}
			assert.NotContains(t, errOut.String(), "komrad did not log the problem", "a komrad that cannot be reached is not asked to log")
		})
	}
}

// recordedReport is one call of the endpoint every hook event reports through.
type recordedReport struct {
	agentID   string
	event     string
	hook      string
	tool      string
	turnError string
}

type fakeHookServerParams struct {
	logged  *[]string
	reports *[]recordedReport
}

func fakeHookServer(t *testing.T, params fakeHookServerParams) string {
	t.Helper()

	logPath, err := toolPath(logTool)
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc(agentv1connect.AgentServiceRecordReportProcedure, func(w http.ResponseWriter, r *http.Request) {
		var report struct {
			Event     string `json:"event"`
			Hook      string `json:"hook"`
			Tool      string `json:"tool"`
			TurnError string `json:"turnError"`
		}
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&report))
		*params.reports = append(*params.reports, recordedReport{
			agentID:   caller.FromHeader(r.Header).AgentID,
			event:     report.Event,
			hook:      report.Hook,
			tool:      report.Tool,
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
		wantProblem  bool
	}{
		{
			name: "a caller that is not an agent has nothing to stamp",
			args: []string{`{"type":"agent-turn-complete"}`},
		},
		{
			name:        "a payload that will not parse is reported and records nothing",
			agentID:     "agent-a",
			args:        []string{`{`},
			wantProblem: true,
		},
		{
			name:    "a payload carrying nothing to record stays silent",
			agentID: "agent-a",
			args:    []string{`{"type":"agent-something-else"}`},
		},
		{
			name:         "a completed turn is recorded",
			agentID:      "agent-a",
			args:         []string{`{"type":"agent-turn-complete","turn-id":"t1"}`},
			wantBoundary: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logged []string
			var reports []recordedReport
			addr := fakeHookServer(t, fakeHookServerParams{
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
				assert.Equal(t, recordedReport{agentID: tt.agentID, event: "REPORT_EVENT_TURN_ENDED", hook: "notify"}, reports[0])
			}
			assert.Empty(t, out.String(), "notify reads nothing back, so nothing is printed")
			assert.Equal(t, tt.wantProblem, errs.Len() > 0, errs.String())
			assert.Equal(t, tt.wantProblem, len(logged) == 1, "a problem komrad can hear is logged under the agent")
		})
	}
}

// TestIntegrationStopFailureHookRecordsTheFailure covers the hook Claude runs in place of Stop
// when an API error ends the turn.
func TestIntegrationStopFailureHookRecordsTheFailure(t *testing.T) {
	var logged []string
	var reports []recordedReport
	addr := fakeHookServer(t, fakeHookServerParams{
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
	assert.Equal(t, recordedReport{
		agentID:   "agent-a",
		event:     "REPORT_EVENT_TURN_FAILED",
		hook:      "StopFailure",
		turnError: "Rate limit reached",
	}, reports[0])
	assert.Empty(t, out.String(), "Claude ignores what a StopFailure hook prints")
}
