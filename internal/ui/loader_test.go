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

// TestLoaderStaysStillWithNothingLoading covers the loader tick staying off while no pull request has checks running.
func TestLoaderStaysStillWithNothingLoading(t *testing.T) {
	harness := openNoticed(t, panelHarnessParams{loader: loader.Arcs}, newPullRequested(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN)...)

	assert.False(t, harness.fireLoader())
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
