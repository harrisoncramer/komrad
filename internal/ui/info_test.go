package ui_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestInfo covers the pane below the stage staying shut until something asks for it.
func TestInfo(t *testing.T) {
	harness := openTerminal(t, panelHarnessParams{}, newStagedShellAgent(t.TempDir()))

	assert.Equal(t, "%9", harness.panel.ReadGuestPane(), "the hovered agent has the stage to itself")
	assert.Empty(t, harness.panel.ReadBelowAnchors(), "nothing is split under it")
	assert.NotContains(t, harness.panel.ReadRespawnPanes(), infoPane, "and nothing is paged into a pane below")
}
