package ui

import (
	"fmt"
	"strings"
	"testing"

	"weave/internal/errui"
	agentstore "weave/internal/store/agent"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func archiveBanner() *errui.Banner {
	return &errui.Banner{
		Summary: "could not archive the agent",
		Detail:  "rpc error: the agent is still setting up and cannot be archived yet",
	}
}

// bannerRowWidth is the widest row of the banner as the panel actually drew it, which is what
// says the fold the panel chose rather than the one it was asked for.
func bannerRowWidth(t *testing.T, view string) int {
	t.Helper()

	widest := 0
	for _, row := range strings.Split(ansi.Strip(view), "\n") {
		if !strings.Contains(row, "could not archive") && !strings.Contains(row, "setting up") {
			continue
		}

		widest = max(widest, lipgloss.Width(strings.TrimRight(row, " ")))
	}

	require.Positive(t, widest, "the banner is drawn somewhere in the view")

	return widest
}

// TestBannerTakesTheWholePaneOnAWindowTooNarrowForABlock covers the splitOf floor: a window that
// will not hold the notes beside the list leaves the list the whole pane, so the banner over it
// is folded to the pane rather than to the sidebar that was never drawn.
func TestBannerTakesTheWholePaneOnAWindowTooNarrowForABlock(t *testing.T) {
	tmux := &recordingTmux{width: 65, height: 24}
	m := infoModel(tmux)
	m.width, m.height = 65, 24
	m.listing.agents[0].alive = false
	m.listing.agents[0].agent.Pane = ""
	m.frame.Solo(true)

	_, drawsColumn := m.columnSplit()
	require.False(t, drawsColumn, "a 65-column window will not hold the notes beside the list")
	require.Equal(t, 65, m.paneWidth(), "so the list is drawn in the whole pane")

	m.banner = archiveBanner()

	assert.Equal(t, strings.Count(m.banner.Render(m.width), "\n"), m.chromeLines(),
		"the banner is charged for the rows it takes at the pane's width")
	assert.Greater(t, bannerRowWidth(t, m.View()), 40, "and is drawn across the pane, not in a ribbon")
}

// TestBannerTakesTheWholePaneWhenTheSidebarWillNotHoldItsChrome covers the other refusal: the
// archive keeps the whole window when its list would run off the bottom of a halved sidebar, so
// the banner over that list is folded to the window too.
func TestBannerTakesTheWholePaneWhenTheSidebarWillNotHoldItsChrome(t *testing.T) {
	tmux := &recordingTmux{width: 200, height: 16}
	m := infoModel(tmux)
	m.width, m.height = 200, 16
	m.screen = screenArchive
	m.helpExpanded = true
	m.frame.Solo(true)

	for i := range 5 {
		m.archive.agents = append(m.archive.agents, agentstore.Agent{
			Name:       fmt.Sprintf("archived-%02d", i),
			WorkflowID: "w1",
		})
	}

	_, drawsColumn := m.columnSplit()
	require.False(t, drawsColumn, "an expanded legend stands too tall in a halved sidebar")
	require.Equal(t, 200, m.paneWidth(), "so the archive keeps the whole window")

	m.banner = archiveBanner()

	assert.Equal(t, strings.Count(m.banner.Render(m.width), "\n"), m.chromeLines(),
		"the banner folds to the window it is drawn over")
	assert.Greater(t, bannerRowWidth(t, m.View()), 60)
}

// TestBannerFoldsToTheSidebarBesideABlock keeps the column case honest: a banner drawn beside a
// block belongs to the sidebar, so it folds to the sidebar's width and the rows it takes come
// off the list rather than off the block.
func TestBannerFoldsToTheSidebarBesideABlock(t *testing.T) {
	tmux := &recordingTmux{}
	m := sizedTo(infoModel(tmux), tmux)
	m.tree.cursor = 0
	m.stageSelection()

	split, drawsColumn := m.columnSplit()
	require.True(t, drawsColumn, "a 200-column window holds the notes beside the list")
	require.Equal(t, split.sidebar, m.paneWidth())

	m.banner = archiveBanner()

	assert.Equal(t, split.rows, m.height, "the block gives up no rows for the banner")
	assert.Equal(t, strings.Count(m.banner.Render(split.sidebar), "\n"), m.chromeLines(),
		"and the list is charged exactly what the join prepends")

	rows := strings.Split(m.View(), "\n")
	assert.Len(t, rows, m.height, "the frame is still the height of the pane")
	for _, row := range rows {
		assert.Equal(t, m.width, lipgloss.Width(row), "every row of the frame is squared off")
	}
}
