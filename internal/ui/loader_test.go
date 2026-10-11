package ui_test

import (
	"slices"
	"strings"
	"testing"

	"komrad/internal/loader"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	taskv1 "komrad/proto/gen/task/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"
	"komrad/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestLoaderSpinsWhileChecksRun covers a pull request with checks running rendering the configured loader a frame a tick.
func TestLoaderSpinsWhileChecksRun(t *testing.T) {
	harness := openNoticed(t, panelHarnessParams{loader: loader.Arcs}, newPullRequested(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING)...)

	var rendered []string
	for range 5 {
		rendered = append(rendered, readChecksFrame(t, harness))
		require.True(t, harness.fireLoader(), "the loader stopped ticking with checks still running")
	}

	assert.Equal(t, []string{"◜", "◝", "◞", "◟", "◜"}, rendered)
}

// TestLoaderSpinsForATaskWithoutItsWriter covers a task row wearing its writer's running checks keeping the loader turning once a search leaves the writer out.
func TestLoaderSpinsForATaskWithoutItsWriter(t *testing.T) {
	rows := newPullRequested(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING)
	writer, ok := rows[0].(*agentv1.Agent)
	require.True(t, ok)
	task, ok := rows[3].(*taskv1.Task)
	require.True(t, ok)
	task.Id, task.Slug, task.Title = "t1", "fix-login", "Fix login"
	writer.TaskId = xptr.Ptr("t1")
	writer.TaskSlug = xptr.Ptr("fix-login")
	writer.CreatedAt = "2026-09-20T12:05:00Z"
	other := newAgentAt("checker", "w1", 0)
	other.TaskId = xptr.Ptr("t1")
	other.TaskSlug = xptr.Ptr("fix-login")
	rows = append(rows, other)
	harness := openNoticed(t, panelHarnessParams{loader: loader.Arcs}, rows...)

	harness.press(slices.Concat([]string{"/", "tab"}, spellKeys("checker"), []string{"enter"})...)
	require.Equal(t, []string{"  backend", "◜ └─ fix-login", "     └─ checker"}, readListRows(harness.readFrame()))

	harness.fireLoader()
	assert.True(t, harness.fireLoader(), "the task row's spinner froze")
}

// TestLoaderStopsTicking covers the loader tick going quiet once no rendered row is loading.
func TestLoaderStopsTicking(t *testing.T) {
	tests := []struct {
		name  string
		open  func(t *testing.T) *panelHarness
		leave func(harness *panelHarness)
	}{
		{
			name: "nothing loading never starts it",
			open: openAgentsWithPullRequest(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN),
		},
		{
			name: "checks finishing stop it",
			open: openAgentsWithPullRequest(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING),
			leave: func(harness *panelHarness) {
				harness.watch(buildPuts(newPullRequested(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN)...))
			},
		},
		{
			name: "leaving the agents list stops it",
			open: openAgentsWithPullRequest(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING),
			leave: func(harness *panelHarness) {
				harness.press("tab")
			},
		},
		{
			name: "filtering the row out stops it",
			open: openAgentsWithPullRequest(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING),
			leave: func(harness *panelHarness) {
				harness.press("/", "tab", "z", "q", "x", "enter")
			},
		},
		{
			name: "switching the archive to its schedules stops it",
			open: openArchiveWithChecksRunning,
			leave: func(harness *panelHarness) {
				harness.press("F", "tab", "esc")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := tt.open(t)

			if tt.leave != nil {
				require.True(t, harness.fireLoader(), "the loader never started")
				tt.leave(harness)
				harness.fireLoader()
			}

			assert.False(t, harness.fireLoader())
		})
	}
}

// TestLoaderSpinsWhileAnAgentSetsUp covers the placeholder beside a setting-up agent rendering the configured loader a frame a tick.
func TestLoaderSpinsWhileAnAgentSetsUp(t *testing.T) {
	tests := []struct {
		name  string
		pane  string
		doing string
	}{
		{
			name:  "a worktree still being made",
			doing: "setting up…",
		},
		{
			name:  "a CLI still starting in its pane",
			pane:  "%9",
			doing: "starting…",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := newAgent("a1", "old", "w1")
			agent.State = agentv1.AgentState_AGENT_STATE_SETTING_UP
			agent.PaneId = tt.pane
			harness := openNoticed(t, panelHarnessParams{loader: loader.Arcs}, agent)

			var rendered []string
			for range 3 {
				rendered = append(rendered, readWriteFrame(t, harness.readFrame(), tt.doing))
				require.True(t, harness.fireLoader(), "the loader stopped ticking with the agent still setting up")
			}

			assert.Equal(t, []string{"◜", "◝", "◞"}, rendered)

			harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend"), newRunning()))
			harness.fireLoader()
			assert.False(t, harness.fireLoader(), "the loader goes quiet once the agent is up")
		})
	}
}

// TestLoaderSpinsWhileAWriteIsInFlight covers a held form's notice rendering the configured loader a frame a tick until its write lands.
func TestLoaderSpinsWhileAWriteIsInFlight(t *testing.T) {
	create := workflowv1connect.WorkflowServiceCreateWorkflowProcedure
	harness := newPanelHarness(t, panelHarnessParams{
		width:  160,
		height: 40,
		loader: loader.Arcs,
	})
	answerWorktreeReads(harness)
	harness.watch(buildSnapshot(newFormWorkflow()))
	harness.answer(create, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&workflowv1.CreateWorkflowResponse{Workflow: &workflowv1.Workflow{Id: "w2"}}), nil
	})
	harness.hold(create)
	harness.press(slices.Concat([]string{"space", "w"}, spellKeys("web"), []string{"enter"})...)

	var rendered []string
	for range 3 {
		rendered = append(rendered, readWriteFrame(t, harness.readFrame(), "creating workflow web…"))
		require.True(t, harness.fireLoader(), "the loader stopped ticking with the write in flight")
	}

	assert.Equal(t, []string{"◜", "◝", "◞"}, rendered)

	harness.release(create)
	harness.watch(buildPuts(newLiveWorkflow("w2", "web")))
	harness.fireLoader()
	assert.False(t, harness.fireLoader(), "the loader goes quiet once the write lands")
}

func openAgentsWithPullRequest(state pullrequestv1.PullRequestState) func(t *testing.T) *panelHarness {
	return func(t *testing.T) *panelHarness {
		t.Helper()

		return openNoticed(t, panelHarnessParams{loader: loader.Arcs}, newPullRequested(state)...)
	}
}

func openArchiveWithChecksRunning(t *testing.T) *panelHarness {
	t.Helper()

	rows := newPullRequested(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING)
	agent, ok := rows[0].(*agentv1.Agent)
	require.True(t, ok)
	agent.ArchivedAt = xptr.Ptr(archivedAt)

	return openArchivedAgents(t, append([]proto.Message{newLiveWorkflow("w1", "backend")}, rows...)...)
}

func readChecksFrame(t *testing.T, harness *panelHarness) string {
	t.Helper()

	for _, line := range readNoticeBlock(harness) {
		if strings.Contains(line, "its pull request has checks running") {
			return strings.Fields(ansi.Strip(line))[0]
		}
	}

	require.Fail(t, "no notice says the checks are running")

	return ""
}

func readWriteFrame(t *testing.T, frame, doing string) string {
	t.Helper()

	for _, line := range splitLines(ansi.Strip(frame)) {
		at := strings.Index(line, doing)
		if at < 0 {
			continue
		}

		fields := strings.Fields(line[:at])
		require.NotEmpty(t, fields)

		return fields[len(fields)-1]
	}

	require.Fail(t, "nothing is rendered doing "+doing)

	return ""
}
