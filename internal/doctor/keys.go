package doctor

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"weave/config"
)

var (
	ErrKeyHeldByTmux = &ProblemError{
		Code:    "key_held_by_tmux",
		Message: "tmux answers a key the panel is bound to, so the panel never sees the press",
	}
	ErrKeyOnTwoActions = &ProblemError{
		Code:    "key_on_two_actions",
		Message: "one key is bound to two actions a screen answers, so the panel would refuse to draw",
	}
	ErrFocusKeyUnavailable = &ProblemError{
		Code:    "focus_key_unavailable",
		Message: "the key that moves focus between the panel and an agent's pane cannot be bound",
	}
)

// tmuxNames are the keys tmux and bubbletea call different things.
var tmuxNames = map[string]string{
	" ":         "Space",
	"enter":     "Enter",
	"esc":       "Escape",
	"tab":       "Tab",
	"shift+tab": "BTab",
	"backspace": "BSpace",
	"pgup":      "PageUp",
	"pgdown":    "PageDown",
	"up":        "Up",
	"down":      "Down",
	"left":      "Left",
	"right":     "Right",
	"home":      "Home",
	"end":       "End",
	"delete":    "Delete",
	"insert":    "IC",
}

// newKeybindingsCheck asserts tmux is not holding a key the panel reads.
func newKeybindingsCheck() Check {
	return Check{
		Name:        "keybindings",
		Description: "the panel's keys reach it rather than being answered by tmux first",
		Needs:       []string{"config"},
		Run: func(ctx context.Context, env Env) Result {
			cfg, err := env.LoadConfig()
			if err != nil {
				return fail(fmt.Errorf("%w: %w", ErrConfigUnreadable, err), "", "")
			}

			if env.ValidateKeybindings == nil {
				return fail(fmt.Errorf("%w: nothing was given to validate the keybindings with", ErrCheckPanicked), "", "")
			}
			if err := env.ValidateKeybindings(cfg.Keybindings); err != nil {
				return fail(
					fmt.Errorf("%w: %w", ErrKeyOnTwoActions, err),
					"",
					"give one of the two actions a key of its own",
				)
			}

			rootKeys := readRootKeys(ctx, env)

			if prefix := readTmuxPrefix(ctx, env); prefix == env.FocusKey {
				return fail(
					fmt.Errorf("%w: %s is your tmux prefix, which tmux reads before the root table",
						ErrFocusKeyUnavailable, env.FocusKey),
					"prefix "+prefix,
					"set-option -g prefix to something other than "+env.FocusKey,
				)
			}

			if held, ok := heldByAnythingElse(rootKeys, env.FocusKey); ok {
				return fail(
					fmt.Errorf("%w: %s is bound in your tmux root table to %s",
						ErrFocusKeyUnavailable, env.FocusKey, held),
					env.FocusKey,
					"unbind -n "+env.FocusKey+" in your tmux config",
				)
			}

			for _, key := range boundKeys(cfg.Keybindings) {
				spelled := tmuxSpelling(key)
				if held, ok := heldByAnythingElse(rootKeys, spelled); ok {
					return fail(
						fmt.Errorf("%w: %q is %s to tmux, bound in the root table to %s",
							ErrKeyHeldByTmux, key, spelled, held),
						key,
						"unbind -n "+spelled+" in your tmux config, or bind that action to another key",
					)
				}
			}

			return pass("no panel key is held by tmux")
		},
	}
}

// readRootKeys is what the root table of weave's own tmux server holds, keyed by the key, and is
// empty where there is no server to ask yet.
func readRootKeys(ctx context.Context, env Env) map[string]string {
	held := map[string]string{}

	if env.TmuxSocket == "" {
		return held
	}

	out, err := env.Output(ctx, "tmux", "-L", env.TmuxSocket, "list-keys", "-T", "root")
	if err != nil {
		return held
	}

	for _, line := range strings.Split(out, "\n") {
		key, command, ok := parseRootBinding(line)
		if !ok {
			continue
		}

		held[key] = command
	}

	return held
}

// parseRootBinding reads one line of list-keys output as the key it binds and the command it runs.
func parseRootBinding(line string) (key, command string, ok bool) {
	_, bound, found := strings.Cut(line, "-T root")
	if !found {
		return "", "", false
	}

	key, command, found = strings.Cut(strings.TrimSpace(bound), " ")
	if !found {
		return "", "", false
	}

	return key, strings.TrimSpace(command), true
}

// heldByAnythingElse reports the command the root table runs for a key, ignoring the panel's own
// focus binding.
func heldByAnythingElse(rootKeys map[string]string, key string) (string, bool) {
	command, held := rootKeys[key]
	if !held || isPanelFocusBinding(command) {
		return "", false
	}

	return command, true
}

// isPanelFocusBinding reports whether a root binding is the one the panel holds.
func isPanelFocusBinding(command string) bool {
	return strings.HasPrefix(command, "if-shell") && strings.Contains(command, "#{==:#{pane_id},")
}

func readTmuxPrefix(ctx context.Context, env Env) string {
	if env.TmuxSocket == "" {
		return ""
	}

	out, err := env.Output(ctx, "tmux", "-L", env.TmuxSocket, "show-options", "-gqv", "prefix")
	if err != nil {
		return ""
	}

	return strings.TrimSpace(out)
}

// boundKeys is every key the panel's bindings name, in a settled order.
func boundKeys(keys config.Keybindings) []string {
	var bound []string

	groups := reflect.ValueOf(keys)
	for i := range groups.NumField() {
		group := groups.Field(i)
		for j := range group.NumField() {
			field, ok := group.Field(j).Interface().([]string)
			if !ok {
				continue
			}

			for _, key := range field {
				if !slices.Contains(bound, key) {
					bound = append(bound, key)
				}
			}
		}
	}

	slices.Sort(bound)

	return bound
}

// tmuxSpelling is how tmux names the key a panel binding names.
func tmuxSpelling(key string) string {
	if named, ok := tmuxNames[key]; ok {
		return named
	}

	if control, ok := strings.CutPrefix(key, "ctrl+"); ok {
		return "C-" + control
	}

	if alt, ok := strings.CutPrefix(key, "alt+"); ok {
		return "M-" + alt
	}

	return key
}
