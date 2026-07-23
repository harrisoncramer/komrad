package panel

import "strings"

// bound reports whether the pressed key is one of the keys configured for an
// action.
func bound(pressed string, keys []string) bool {
	for _, k := range keys {
		if k == pressed {
			return true
		}
	}

	return false
}

// hintKeys renders a binding's keys for the help text, joining them with "/"
// and shortening "ctrl+x" to "C-x" so they read like the rest of the hints.
func hintKeys(keys []string) string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = strings.Replace(k, "ctrl+", "C-", 1)
	}

	return strings.Join(out, "/")
}
