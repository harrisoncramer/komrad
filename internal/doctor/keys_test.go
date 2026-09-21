package doctor

import (
	"context"
	"slices"
	"strings"
	"testing"

	"weave/config"
	"weave/internal/ui"

	"github.com/stretchr/testify/assert"
)

// rootKeysEnv answers tmux with a root table holding the given bindings and a prefix of C-b.
func rootKeysEnv(cfg *config.Config, rootKeys ...string) Env {
	return rootKeysEnvWithPrefix(cfg, "C-b", rootKeys...)
}

func rootKeysEnvWithPrefix(cfg *config.Config, prefix string, rootKeys ...string) Env {
	env := configEnv(cfg, nil, true)
	env.TmuxSocket = "weave"
	env.FocusKey = ui.FocusKey
	env.ValidateKeybindings = ui.ValidateKeybindings
	env.Output = func(_ context.Context, _ string, args ...string) (string, error) {
		switch {
		case slices.Contains(args, "list-keys"):
			lines := make([]string, 0, len(rootKeys))
			for _, key := range rootKeys {
				lines = append(lines, "bind-key  -T root "+key+"    copy-mode -u")
			}

			return strings.Join(lines, "\n"), nil
		case slices.Contains(args, "prefix"):
			return prefix + "\n", nil
		}

		return "", nil
	}

	return env
}

// TestKeybindingsCheckFindsAKeyTmuxHolds covers a panel binding on a key the user's own tmux takes
// in its root table, where the press never reaches the panel at all.
func TestKeybindingsCheckFindsAKeyTmuxHolds(t *testing.T) {
	cfg := config.Defaults()
	cfg.Keybindings.Agents.Search = []string{"ctrl+y"}

	result := newKeybindingsCheck().Run(t.Context(), rootKeysEnv(cfg, "C-y"))

	assert.Equal(t, StatusFail, result.Status,
		"tmux answers C-y before the panel does, so search is unreachable")
	assert.Equal(t, "ctrl+y", result.Detail)
	assert.Contains(t, result.Fix, "unbind -n C-y")
}

// TestKeybindingsCheckPassesWhenTmuxHoldsNothing covers the ordinary machine, where the root table
// holds only mouse bindings.
func TestKeybindingsCheckPassesWhenTmuxHoldsNothing(t *testing.T) {
	result := newKeybindingsCheck().Run(t.Context(), rootKeysEnv(config.Defaults(), "MouseDown1Pane"))

	assert.Equal(t, StatusOK, result.Status)
}

// TestKeybindingsCheckIgnoresThePanelsOwnFocusBinding covers the focus key the panel binds itself.
func TestKeybindingsCheckIgnoresThePanelsOwnFocusBinding(t *testing.T) {
	env := configEnv(config.Defaults(), nil, true)
	env.TmuxSocket = "weave"
	env.FocusKey = ui.FocusKey
	env.ValidateKeybindings = ui.ValidateKeybindings
	env.Output = func(_ context.Context, _ string, args ...string) (string, error) {
		if slices.Contains(args, "list-keys") {
			return `bind-key  -T root C-a    if-shell -F #{==:#{pane_id},%3} "select-pane -t %9" "select-pane -t %3"`, nil
		}

		return "C-b\n", nil
	}

	result := newKeybindingsCheck().Run(t.Context(), env)

	assert.Equal(t, StatusOK, result.Status)
}

// TestKeybindingsCheckIgnoresAKeyThePanelHasTaken covers a root binding the panel made itself,
// which hands the key through on its own pane and runs the user's command elsewhere.
func TestKeybindingsCheckIgnoresAKeyThePanelHasTaken(t *testing.T) {
	env := configEnv(config.Defaults(), nil, true)
	env.TmuxSocket = "weave"
	env.FocusKey = ui.FocusKey
	env.ValidateKeybindings = ui.ValidateKeybindings
	env.Output = func(_ context.Context, _ string, args ...string) (string, error) {
		if slices.Contains(args, "list-keys") {
			return `bind-key  -T root C-d    if-shell -F "#{==:#{pane_id},#{@weave_panel_pane}}" send-keys "copy-mode -u"`, nil
		}

		return "C-b\n", nil
	}

	result := newKeybindingsCheck().Run(t.Context(), env)

	assert.Equal(t, StatusOK, result.Status)
	assert.Equal(t, "no panel key is held by tmux", result.Detail)
}

// TestKeybindingsCheckWaitsForThePanelToTakeAKey covers the user's binding standing on a panel
// key while no panel is running, which the next panel rebinds as it starts.
func TestKeybindingsCheckWaitsForThePanelToTakeAKey(t *testing.T) {
	env := rootKeysEnv(config.Defaults(), "C-d", "C-u")
	env.IsPanelRunning = func(context.Context) bool { return false }

	result := newKeybindingsCheck().Run(t.Context(), env)

	assert.Equal(t, StatusOK, result.Status)
	assert.Equal(t, "the panel takes C-d, C-u for its own pane when it opens", result.Detail)
}

// TestKeybindingsCheckFindsAKeyThePanelCouldNotTake covers the user's binding still standing on
// a panel key with the panel up, which is the rebinding having been refused or undone.
func TestKeybindingsCheckFindsAKeyThePanelCouldNotTake(t *testing.T) {
	env := rootKeysEnv(config.Defaults(), "C-d")
	env.IsPanelRunning = func(context.Context) bool { return true }

	result := newKeybindingsCheck().Run(t.Context(), env)

	assert.Equal(t, StatusFail, result.Status)
	assert.Equal(t, "ctrl+d", result.Detail)
	assert.Contains(t, result.Fix, "unbind -n C-d")
}

// TestKeybindingsCheckFindsAPrefixOnTheFocusKey covers a prefix set to the focus key, which tmux
// reads before the root table, so the focus key installs and never fires.
func TestKeybindingsCheckFindsAPrefixOnTheFocusKey(t *testing.T) {
	result := newKeybindingsCheck().Run(t.Context(), rootKeysEnvWithPrefix(config.Defaults(), ui.FocusKey))

	assert.Equal(t, StatusFail, result.Status, "the prefix is read first, so the focus key is dead")
	assert.Contains(t, result.Fix, "prefix")
}

// TestKeybindingsCheckFindsTheFocusKeyTakenByTmux covers the focus key already bound in the root
// table by the user's own tmux config, which the panel would overwrite and then give back.
func TestKeybindingsCheckFindsTheFocusKeyTakenByTmux(t *testing.T) {
	result := newKeybindingsCheck().Run(t.Context(), rootKeysEnv(config.Defaults(), ui.FocusKey))

	assert.Equal(t, StatusFail, result.Status)
	assert.Contains(t, result.Fix, "unbind -n "+ui.FocusKey)
}

// TestTheKeybindingsCheckIsRun pins the check being one doctor makes.
func TestTheKeybindingsCheckIsRun(t *testing.T) {
	checks := Checks(Settings{Harnesses: claudeAndCodex, TmuxSocket: "weave"})

	assert.True(t, slices.ContainsFunc(checks, func(c Check) bool { return c.Name == "keybindings" }))
}
