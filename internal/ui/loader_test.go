package ui_test

import (
	"strings"
	"testing"

	"komrad/internal/loader"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		state pullrequestv1.PullRequestState
		leave func(harness *panelHarness)
	}{
		{
			name:  "nothing loading never starts it",
			state: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
		},
		{
			name:  "checks finishing stop it",
			state: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING,
			leave: func(harness *panelHarness) {
				harness.watch(buildPuts(newPullRequested(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN)...))
			},
		},
		{
			name:  "leaving the agents list stops it",
			state: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING,
			leave: func(harness *panelHarness) {
				harness.press("tab", "s")
			},
		},
		{
			name:  "filtering the row out stops it",
			state: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING,
			leave: func(harness *panelHarness) {
				harness.press("/", "tab", "z", "q", "x", "enter")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openNoticed(t, panelHarnessParams{loader: loader.Arcs}, newPullRequested(tt.state)...)

			if tt.leave != nil {
				require.True(t, harness.fireLoader(), "the loader never started")
				tt.leave(harness)
				harness.fireLoader()
			}

			assert.False(t, harness.fireLoader())
		})
	}
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
