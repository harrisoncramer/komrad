package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"komrad/config"
	"komrad/internal/harness"
	"komrad/internal/harness/claude"
	"komrad/internal/harness/codex"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	taskv1 "komrad/proto/gen/task/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
)

// TestCompactedHookRemindsTheAgent pins what the hook hands an agent once it has compacted.
func TestCompactedHookRemindsTheAgent(t *testing.T) {
	tests := []struct {
		name            string
		row             *agentv1.GetAgentResponse
		threads         []*agentv1.ReviewThread
		wantThreadsTask string
		wantText        []string
		wantAbsent      []string
	}{
		{
			name: "a writer is handed its branch, pull request, open threads and task",
			row: &agentv1.GetAgentResponse{
				Agent: &agentv1.Agent{
					Id:     "agent-a",
					TaskId: xptr.Ptr("task-a"),
					Name:   "compaction",
					Role:   agentv1.AgentRole_AGENT_ROLE_WRITER,
					Prompt: "build the usage block",
				},
				Task:       &taskv1.Task{Branch: xptr.Ptr("agent/compaction")},
				UnreadMail: true,
				PullRequests: []*pullrequestv1.PullRequest{{
					Url:   "https://github.com/o/r/pull/9",
					State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED,
				}},
			},
			threads: []*agentv1.ReviewThread{
				{
					Id:       "thread-open",
					FilePath: "config/appconfig.go",
					NewStart: 10,
					NewEnd:   12,
				},
				{
					Id:         "thread-settled",
					FilePath:   "config/appconfig.go",
					ResolvedAt: "2026-09-25T10:00:00Z",
				},
			},
			wantThreadsTask: "task-a",
			wantText: []string{
				"You are compaction (agent-a), the writer agent.",
				"Branch: agent/compaction",
				"Pull request: https://github.com/o/r/pull/9 (changes requested)",
				"Open review threads on your work (1), answered with add_review_message",
				"- thread-open on config/appconfig.go lines 10-12",
				"Call read_mail.",
				"build the usage block",
			},
			wantAbsent: []string{"thread-settled"},
		},
		{
			name: "a reviewer is handed the threads on the work it reads",
			row: &agentv1.GetAgentResponse{Agent: &agentv1.Agent{
				Id:     "agent-r",
				TaskId: xptr.Ptr("task-a"),
				Name:   "compaction-review",
				Role:   agentv1.AgentRole_AGENT_ROLE_REVIEWER,
			}, Task: &taskv1.Task{Slug: "compaction"}},
			threads: []*agentv1.ReviewThread{{
				Id:       "thread-open",
				FilePath: "a.go",
				NewStart: 1,
				NewEnd:   1,
			}},
			wantThreadsTask: "task-a",
			wantText: []string{
				"the reviewer agent",
				"Task: compaction",
				"Your open threads on the work you review (1)",
				"- thread-open on a.go lines 1-1",
			},
			wantAbsent: []string{"read_mail", "add_review_message"},
		},
		{
			name: "an orchestrator has no diff, so no threads are read",
			row: &agentv1.GetAgentResponse{Agent: &agentv1.Agent{
				Id:   "agent-o",
				Name: "orchestrator",
				Role: agentv1.AgentRole_AGENT_ROLE_ORCHESTRATOR,
			}},
			wantText:   []string{"the orchestrator agent"},
			wantAbsent: []string{"Open review threads"},
		},
		{
			name: "a long task is cut and says where the rest is",
			row: &agentv1.GetAgentResponse{Agent: &agentv1.Agent{
				Id:     "agent-a",
				TaskId: xptr.Ptr("task-a"),
				Name:   "long",
				Role:   agentv1.AgentRole_AGENT_ROLE_WRITER,
				Prompt: strings.Repeat("x", promptLimit+50),
			}},
			wantThreadsTask: "task-a",
			wantText:        []string{strings.Repeat("x", promptLimit) + "\n(cut here; get_agent returns the whole task)"},
			wantAbsent:      []string{strings.Repeat("x", promptLimit+1)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var threadsTask string
			var reportedEvent, reportedHook string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				assert.NoError(t, err)

				var out []byte
				switch r.URL.Path {
				case agentv1connect.AgentServiceGetAgentProcedure:
					out, err = protojson.Marshal(tt.row)
				case agentv1connect.AgentServiceListReviewThreadsProcedure:
					var req struct {
						Task string `json:"task_id"`
					}
					assert.NoError(t, json.Unmarshal(body, &req))
					threadsTask = req.Task
					out, err = protojson.Marshal(&agentv1.ListReviewThreadsResponse{Threads: tt.threads})
				case agentv1connect.AgentServiceRecordReportProcedure:
					var req struct {
						Event string `json:"event"`
						Hook  string `json:"hook"`
					}
					assert.NoError(t, json.Unmarshal(body, &req))
					reportedEvent, reportedHook = req.Event, req.Hook
					out = []byte(`{}`)
				default:
					t.Errorf("unexpected call to %s", r.URL.Path)
				}
				assert.NoError(t, err)
				_, err = w.Write(out)
				assert.NoError(t, err)
			}))
			t.Cleanup(server.Close)

			state := &app{
				env: &config.Environment{
					Addr:    strings.TrimPrefix(server.URL, "http://"),
					AgentID: tt.row.GetAgent().GetId(),
				},
				harnesses: hookHarnesses(t),
			}

			var out bytes.Buffer
			cmd := compactedHookCmd(state)
			cmd.SetIn(bytes.NewBufferString(`{"source":"compact"}`))
			cmd.SetOut(&out)
			cmd.SetArgs([]string{})
			require.NoError(t, cmd.Execute())

			assert.Equal(t, tt.wantThreadsTask, threadsTask, "threads are read for the diff the agent works on")
			assert.Equal(t, "REPORT_EVENT_COMPACTED", reportedEvent, "the compaction is recorded")
			assert.Equal(t, "SessionStart", reportedHook)

			var printed hookOutput
			require.NoError(t, json.Unmarshal(out.Bytes(), &printed))
			assert.Equal(t, string(claude.EventSessionStart), printed.HookSpecificOutput.HookEventName)
			for _, want := range tt.wantText {
				assert.Contains(t, printed.HookSpecificOutput.AdditionalContext, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, printed.HookSpecificOutput.AdditionalContext, absent)
			}
		})
	}
}

// TestCompactedHookPrintsNothingWithoutARecord covers the hook staying silent when it has nothing to say.
func TestCompactedHookPrintsNothingWithoutARecord(t *testing.T) {
	tests := []struct {
		name    string
		agentID string
		addr    string
	}{
		{
			name: "a caller that is not an agent has no record",
			addr: "127.0.0.1:1",
		},
		{
			name:    "a panel that is not there",
			agentID: "agent-a",
			addr:    "127.0.0.1:1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &app{
				env:       &config.Environment{Addr: tt.addr, AgentID: tt.agentID},
				harnesses: hookHarnesses(t),
			}

			var out bytes.Buffer
			cmd := compactedHookCmd(state)
			cmd.SetIn(bytes.NewBufferString(`{"source":"compact"}`))
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{})

			require.NoError(t, cmd.Execute())
			assert.Empty(t, out.String())
		})
	}
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
