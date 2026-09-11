package ui

import (
	"fmt"
	"strings"
	"testing"

	agentstore "weave/internal/store/agent"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
)

// TestNoScreenDrawsARowWiderThanItsPane sweeps every screen across the pane widths a window or a
// configured sidebar could leave, since a row wider than the pane wraps in the terminal and costs
// the frame the row at the bottom.
func TestNoScreenDrawsARowWiderThanItsPane(t *testing.T) {
	for s := screenAgents; s <= screenMail; s++ {
		t.Run(fmt.Sprintf("screen %d", s), func(t *testing.T) {
			widest, example := 0, ""

			for width := 5; width <= 120; width++ {
				for _, height := range []int{12, 40} {
					for _, expanded := range []bool{false, true} {
						m := longNamedModel(t)
						m.screen = s
						m.width, m.height = width, height
						m.helpExpanded = expanded

						for i, row := range drawnLines(m.View()) {
							if lipgloss.Width(row) <= width || width < widest {
								continue
							}

							widest = width
							example = fmt.Sprintf("row %d at %dx%d is %d columns: %q", i, width, height, lipgloss.Width(row), row)
						}
					}
				}
			}

			assert.Zero(t, widest, "a row is wider than its pane at every width up to %d, widest %s", widest, example)
		})
	}
}

// longNamedModel is the crowded panel with a name on every listing longer than any pane, which is
// what finds a row drawn without a cut at all rather than one that only overruns a narrow pane.
func longNamedModel(t *testing.T) model {
	t.Helper()

	long := strings.Repeat("x", 120)

	m := crowdedModel(t)
	m.listing.workflows[0].Name = long
	m.archive.workflows = m.listing.workflows
	m.schedules.items[0].schedule.Name = long
	m.listing.agents = append([]agentItem{{
		agent: agentstore.Agent{ID: "zz", Name: long, WorkflowID: "w1"},
	}}, m.listing.agents...)

	return m
}
