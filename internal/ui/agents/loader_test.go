package agents_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"komrad/internal/loader"
	"komrad/internal/ui/paneltest"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	taskv1 "komrad/proto/gen/task/v1"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestLoaderSpinsWhileChecksRun covers a pull request with checks running rendering the configured loader a frame a tick.
func TestLoaderSpinsWhileChecksRun(t *testing.T) {
	harness := openNoticed(t, paneltest.Params{Loader: loader.Arcs}, newPullRequested(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING)...)

	var rendered []string
	for range 5 {
		rendered = append(rendered, readChecksFrame(t, harness))
		require.True(t, harness.IsSpinning(), "the loader stopped ticking with checks still running")
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
	writer.CreatedAt = timestamppb.New(time.Date(2026, 9, 20, 12, 5, 0, 0, time.UTC))
	other := paneltest.AgentAt("checker", "w1", 0)
	other.TaskId = xptr.Ptr("t1")
	other.TaskSlug = xptr.Ptr("fix-login")
	rows = append(rows, other)
	harness := openNoticed(t, paneltest.Params{Loader: loader.Arcs}, rows...)

	harness.Press(slices.Concat([]string{"/", "tab"}, strings.Split("checker", ""), []string{"enter"})...)
	require.Equal(t, []string{"  backend", "◜ └─ fix-login", "     └─ checker"}, paneltest.ListRows(harness.Frame()))

	harness.Advance(loader.Interval)
	assert.True(t, harness.IsSpinning(), "the task row's spinner froze")
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
			agent := paneltest.Agent("a1", "old", "w1")
			agent.State = agentv1.AgentState_AGENT_STATE_SETTING_UP
			agent.PaneId = tt.pane
			harness := openNoticed(t, paneltest.Params{Loader: loader.Arcs}, agent)

			var rendered []string
			for range 3 {
				rendered = append(rendered, readWriteFrame(t, harness.Frame(), tt.doing))
				require.True(t, harness.IsSpinning(), "the loader stopped ticking with the agent still setting up")
			}

			assert.Equal(t, []string{"◜", "◝", "◞"}, rendered)

			harness.Watch(paneltest.Snapshot(paneltest.Workflow("w1", "backend"), paneltest.Running()))
			harness.Advance(loader.Interval)
			assert.False(t, harness.IsSpinning(), "the loader goes quiet once the agent is up")
		})
	}
}

func readChecksFrame(t *testing.T, harness *paneltest.Panel) string {
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

	for _, line := range paneltest.SplitLines(ansi.Strip(frame)) {
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
