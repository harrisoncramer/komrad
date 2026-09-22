package ui

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"weave/internal/ui/panel"
	"weave/lib/xptr"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// updateGoldens rewrites the golden files.
var updateGoldens = flag.Bool("update", false, "rewrite the golden files")

// TestNoticeBlockGolden draws the record's notice block whole.
func TestNoticeBlockGolden(t *testing.T) {
	restore := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })
	lipgloss.SetColorProfile(termenv.ANSI)

	tests := []struct {
		name string
		item func(*panel.AgentRow)
	}{
		{
			name: "marks-beside-the-name",
			item: func(item *panel.AgentRow) {
				item.UnreadMail = true
				item.FailedRun = true
				item.Agent.HeldAt = xptr.Ptr(time.Unix(0, 0))
			},
		},
		{
			name: "column-facts",
			item: func(item *panel.AgentRow) {
				item.Agent.SequenceInProgress = true
				item.LocalVerdict = agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED
			},
		},
		{
			name: "a-failed-create",
			item: func(item *panel.AgentRow) {
				item.Agent.State = panel.StateFailedToCreate
				item.Agent.WorktreeRemoved = true
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := detailsModel(&recordingTmux{})
			m.height, m.width = 40, 60
			tt.item(&m.listing.agents[0])

			requireNoticeGolden(t, tt.name, strings.Join(m.detailsNotices(m.hoveredDetails(), m.detailsLines()-3), "\n"))
		})
	}
}

// requireNoticeGolden compares a drawn block against the file holding it, writing that file
// instead when the suite was run to update them.
func requireNoticeGolden(t *testing.T, name, got string) {
	t.Helper()

	requireGolden(t, "notices-"+name, got)
}

// requireGolden compares a drawn block against the file named for it under testdata, writing
// that file instead when the suite was run to update them.
func requireGolden(t *testing.T, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", name+".golden")
	if *updateGoldens {
		require.NoError(t, os.MkdirAll("testdata", 0o750))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o600))

		return
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err, "no golden file yet, so run the suite with -update")
	assert.Equal(t, string(want), got)
}
