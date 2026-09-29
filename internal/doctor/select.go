package doctor

import "slices"

// The names of the checks a caller picks out of a list by name.
const (
	CheckConfig         = "config"
	CheckDefaultHarness = "default harness"
	CheckContext        = "context"
	CheckTmux           = "tmux"
	CheckTmuxVersion    = "tmux version"
	CheckTmuxShell      = "tmux shell"
)

// Select is the checks among these with one of the names, in the order they were given.
func Select(checks []Check, names ...string) []Check {
	selected := make([]Check, 0, len(names))
	for _, check := range checks {
		if slices.Contains(names, check.Name) {
			selected = append(selected, check)
		}
	}

	return selected
}
