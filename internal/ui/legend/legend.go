// Package legend holds the bindings a legend renders under a screen.
package legend

import "charm.land/bubbles/v2/key"

// Help is a legend's bindings: the short strip, and the full table the help key expands it to.
type Help struct {
	Short []key.Binding
	Full  []key.Binding
}
