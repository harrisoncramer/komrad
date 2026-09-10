package doctor

import (
	"context"
	"slices"
	"strings"
	"testing"

	"weave/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rootKeysEnv answers list-keys with a root table holding the given bindings.
func rootKeysEnv(cfg *config.Config, rootKeys ...string) Env {
	env := configEnv(cfg, nil, true)
	env.Output = func(_ context.Context, _ string, args ...string) (string, error) {
		if !slices.Contains(args, "list-keys") {
			return "", nil
		}

		var lines []string
		for _, key := range rootKeys {
			lines = append(lines, "bind-key  -T root "+key+"    copy-mode -u")
		}

		return strings.Join(lines, "\n"), nil
	}

	return env
}

// keybindingsCheck is the check that reads the panel's bindings back against tmux, found by
// name so this test says what is missing rather than failing to compile.
func keybindingsCheck(t *testing.T) Check {
	t.Helper()

	checks := Checks(Settings{Harnesses: claudeAndCodex, TmuxSocket: "weave"})
	i := slices.IndexFunc(checks, func(c Check) bool { return c.Name == "keybindings" })
	require.GreaterOrEqual(t, i, 0, "doctor makes no assertion about the panel's keybindings")

	return checks[i]
}

// TestKeybindingsCheckFindsAKeyTmuxHolds covers a panel binding on a key the user's own tmux
// takes in its root table, where the press never reaches the panel at all.
func TestKeybindingsCheckFindsAKeyTmuxHolds(t *testing.T) {
	cfg := config.Defaults()
	cfg.Keybindings.Agents.Search = []string{"ctrl+d"}

	result := keybindingsCheck(t).Run(t.Context(), rootKeysEnv(cfg, "C-d"))

	assert.Equal(t, StatusFail, result.Status,
		"tmux answers C-d before the panel does, so search is unreachable")
	assert.Contains(t, result.Detail, "ctrl+d")
}

// TestKeybindingsCheckPassesWhenTmuxHoldsNothing covers the ordinary machine, where the root
// table holds only mouse bindings.
func TestKeybindingsCheckPassesWhenTmuxHoldsNothing(t *testing.T) {
	result := keybindingsCheck(t).Run(t.Context(), rootKeysEnv(config.Defaults(), "MouseDown1Pane"))

	assert.Equal(t, StatusOK, result.Status)
}

// TestKeybindingsCheckFindsAFocusKeyTmuxRefuses covers focus_key spelled the way the panel's
// own bindings are, which tmux calls an unknown key and the panel only logs.
func TestKeybindingsCheckFindsAFocusKeyTmuxRefuses(t *testing.T) {
	cfg := config.Defaults()
	cfg.Panel.FocusKey = "ctrl+a"

	result := keybindingsCheck(t).Run(t.Context(), rootKeysEnv(cfg))

	assert.Equal(t, StatusFail, result.Status, "tmux would refuse this key, and the focus key would do nothing")
	assert.Contains(t, result.Fix, "C-a")
}

// TestKeybindingsCheckFindsAFocusKeyOnThePrefix covers focus_key set to the tmux prefix, which
// tmux reads as the prefix before it reads the root table, so the binding never fires.
func TestKeybindingsCheckFindsAFocusKeyOnThePrefix(t *testing.T) {
	cfg := config.Defaults()
	cfg.Panel.FocusKey = "C-]"

	env := rootKeysEnv(cfg)
	env.Output = func(_ context.Context, _ string, args ...string) (string, error) {
		if slices.Contains(args, "show-options") {
			return "prefix C-]", nil
		}

		return "", nil
	}

	result := keybindingsCheck(t).Run(t.Context(), env)

	assert.Equal(t, StatusFail, result.Status, "the prefix is read first, so the focus key is dead")
}

// TestKeybindingsCheckFindsAPrintableFocusKey covers focus_key on a key an agent types, bound in
// the root table and so taken from every pane on weave's own server.
func TestKeybindingsCheckFindsAPrintableFocusKey(t *testing.T) {
	cfg := config.Defaults()
	cfg.Panel.FocusKey = "j"

	result := keybindingsCheck(t).Run(t.Context(), rootKeysEnv(cfg))

	assert.Equal(t, StatusFail, result.Status,
		"a root binding on j takes it from the panel's own movement and from every agent's CLI")
}
