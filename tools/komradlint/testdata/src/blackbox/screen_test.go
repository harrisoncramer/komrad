package blackbox_test

import (
	"testing"

	"blackbox"
)

func TestScreen(t *testing.T) {
	screen := blackbox.NewScreen() // want `a story in blackbox_test drives Screen; drive it through the harness`
	screen.Focus()
}
