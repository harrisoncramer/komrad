package ui

import (
	"testing"

	"weave/config"
	"weave/internal/errui"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAlertHistoryOpensFromTheAgentsList covers the key that opens the history screen, and
// that dismissed alerts are what it shows.
func TestAlertHistoryOpensFromTheAgentsList(t *testing.T) {
	m := model{screen: screenAgents, keys: config.DefaultKeybindings()}
	m.alerts.push(errui.LevelError, "first failure", "boom")
	m.alerts.dismissNewest()

	updated, _ := m.updateAgents(keyMsg(m.keys.Alerts.Open[0]))
	got, ok := updated.(model)
	require.True(t, ok)

	assert.Equal(t, screenAlerts, got.screen)
	assert.Contains(t, got.alertsView(), "first failure")
	assert.Contains(t, got.alertsView(), "boom")
}

// TestAlertHistoryBackReturnsToAgents covers the way out, the same way every other screen
// reached from the agents list goes back to it.
func TestAlertHistoryBackReturnsToAgents(t *testing.T) {
	m := model{screen: screenAlerts, keys: config.DefaultKeybindings(), stage: NewStage(&recordingTmux{}, config.DefaultLayout())}

	updated, _ := m.updateAlerts(keyMsg("esc"))
	got, ok := updated.(model)
	require.True(t, ok)

	assert.Equal(t, screenAgents, got.screen)
}

// TestAlertHistoryEmptyStateSaysSo covers the screen before anything has ever left the
// queue, rather than rendering a blank list.
func TestAlertHistoryEmptyStateSaysSo(t *testing.T) {
	m := model{screen: screenAlerts, keys: config.DefaultKeybindings()}

	assert.Contains(t, m.alertsView(), "nothing dismissed yet")
}

// TestAlertHistoryNavMovesTheCursor covers moving through more than one dismissed alert,
// clamped at either end.
func TestAlertHistoryNavMovesTheCursor(t *testing.T) {
	m := model{screen: screenAlerts, keys: config.DefaultKeybindings()}
	m.alerts.push(errui.LevelError, "first", "")
	m.alerts.dismissNewest()
	m.alerts.push(errui.LevelError, "second", "")
	m.alerts.dismissNewest()

	updated, _ := m.updateAlerts(keyMsg("j"))
	got, ok := updated.(model)
	require.True(t, ok)
	assert.Equal(t, 1, got.alertHistory.cursor)

	updated, _ = got.updateAlerts(keyMsg("j"))
	got, ok = updated.(model)
	require.True(t, ok)
	assert.Equal(t, 1, got.alertHistory.cursor, "the cursor stays on the last row")

	updated, _ = got.updateAlerts(keyMsg("k"))
	got, ok = updated.(model)
	require.True(t, ok)
	assert.Equal(t, 0, got.alertHistory.cursor)
}
