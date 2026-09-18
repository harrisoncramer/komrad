package ui

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
	"weave/lib/xptr"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// updateNotices rewrites the golden files holding the drawn record.
var updateNotices = flag.Bool("update", false, "rewrite the golden files")

// TestNoticeBlockGolden draws the record's notice block whole, which is where a glyph gone wrong
// shows up as itself rather than as a name in an assertion.
func TestNoticeBlockGolden(t *testing.T) {
	restore := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })
	lipgloss.SetColorProfile(termenv.ANSI)

	tests := []struct {
		name string
		item func(*agentItem)
	}{
		{
			name: "marks-beside-the-name",
			item: func(item *agentItem) {
				item.unreadMail = true
				item.failedRun = true
				item.agent.HeldAt = xptr.Ptr(time.Unix(0, 0))
			},
		},
		{
			name: "column-facts",
			item: func(item *agentItem) {
				item.agent.SequenceInProgress = true
				item.localVerdict = string(reviewstore.VerdictChangesRequested)
				item.localRounds = 2
			},
		},
		{
			name: "a-failed-create",
			item: func(item *agentItem) {
				item.agent.State = agentstore.StateFailedToCreate
				item.agent.WorktreeRemoved = true
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

	path := filepath.Join("testdata", "notices-"+name+".golden")
	if *updateNotices {
		require.NoError(t, os.MkdirAll("testdata", 0o750))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o600))

		return
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err, "no golden file yet, so run the suite with -update")
	assert.Equal(t, string(want), got)
}
