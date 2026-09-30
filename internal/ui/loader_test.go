package ui_test

import (
	"strings"
	"testing"

	"komrad/internal/loader"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestLoaderSpinsWhileChecksRun covers a pull request with checks running drawing the configured loader a frame a tick.
func TestLoaderSpinsWhileChecksRun(t *testing.T) {
	harness := openNoticed(t, panelHarnessParams{loader: loader.Arcs}, newPullRequested(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING)...)

	var drawn []string
	for range 5 {
		drawn = append(drawn, readChecksFrame(t, harness))
		require.True(t, harness.fireLoader(), "the loader stopped ticking with checks still running")
	}

	assert.Equal(t, []string{"◜", "◝", "◞", "◟", "◜"}, drawn)
}

// TestLoaderStopsTicking covers the loader tick going quiet once no drawn row is loading.
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
				harness.press("tab", "tab")
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

	return openGroupedArchive(t, append([]proto.Message{newLiveWorkflow("w1", "backend")}, rows...)...)
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
