package doctor

import (
	"testing"

	"weave/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChecksAreWellFormed covers the wiring of each list rather than any one assertion: a
// typo in a dependency name would silently run a check meant to be skipped, a duplicate
// name would read the wrong check's conclusion, and a dependency listed only on the other
// machine would skip a check the service makes on its own; none of which shows up at runtime
// as more than a confusing report.
func TestChecksAreWellFormed(t *testing.T) {
	settings := Settings{Harnesses: []HarnessInfo{
		{ID: "claude", Binary: "claude"},
		{ID: "codex", Binary: "codex"},
	}}

	tests := []struct {
		name   string
		checks []Check
	}{
		{
			name:   "here",
			checks: HereChecks(),
		},
		{
			name:   "engine",
			checks: EngineChecks(settings),
		},
		{
			name:   "both, when the engine is this machine",
			checks: Checks(settings),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NotEmpty(t, tt.checks)

			seen := make(map[string]bool, len(tt.checks))
			for _, check := range tt.checks {
				assert.NotEmpty(t, check.Name)
				assert.NotEmpty(t, check.Description, "%s says what it is for", check.Name)
				assert.NotNil(t, check.Run, "%s has something to run", check.Name)

				assert.False(t, seen[check.Name], "no two checks answer to %q", check.Name)
				seen[check.Name] = true
			}

			concluded := make(map[string]bool, len(tt.checks))
			for _, check := range tt.checks {
				for _, needed := range check.Needs {
					assert.True(t, concluded[needed],
						"%q needs %q, which has to be a check listed before it in the same list", check.Name, needed)
				}
				concluded[check.Name] = true
			}
		})
	}
}

// TestChecksMakesEveryCheckOnce covers the doctor on a machine that is its own engine: the
// checks both lists hold are made once, and the engine's come after the ones about here.
func TestChecksMakesEveryCheckOnce(t *testing.T) {
	settings := Settings{Harnesses: []HarnessInfo{{ID: "claude", Binary: "claude"}}}

	names := xslice.Map(Checks(settings), func(check Check) string {
		return check.Name
	})

	assert.Equal(t, []string{
		"tmux",
		"tmux version",
		"config",
		"keybindings",
		"server",
		"forges",
		"terminal glyphs",
		"terminal emphasis",
		"terminal color",
		"tmux shell",
		"default harness",
		"hooks",
		"worktrees",
		"context",
		"database",
		"bases",
		"schema",
		"readonly login",
		"orphaned worktrees",
		"claude",
	}, names)
}
