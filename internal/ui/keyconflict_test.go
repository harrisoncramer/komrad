package ui_test

import (
	"testing"

	"weave/config"

	"github.com/stretchr/testify/assert"
)

// TestKeyConflictExpectsEveryGlobalKey covers the keybinding check expecting each key the router answers first.
func TestKeyConflictExpectsEveryGlobalKey(t *testing.T) {
	pressable := readConfiguredKeys(t, config.DefaultKeybindings())

	for _, screen := range buildDriftScreens() {
		t.Run("every key answered ahead of the "+screen.name+" screen is one the keybinding check expects", func(t *testing.T) {
			harness := openDriftScreen(t, screen)
			modelled := harness.panel.ReadGlobalArmKeys()

			for _, pressed := range pressable {
				if !harness.panel.IsGlobalKey(pressed) {
					continue
				}

				assert.Contains(t, modelled, pressed, "the router answers %q ahead of the %s screen and the keybinding check does not expect it", pressed, screen.name)
			}
		})
	}
}
