package doctor

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"komrad/internal/tmux"
	"komrad/lib/xslice"
)

var (
	errKeyHeldByTmux = &ProblemError{
		Code:    "key_held_by_tmux",
		Message: "tmux answers a key the panel is bound to, so the panel never sees the press",
	}
	errKeyOnTwoActions = &ProblemError{
		Code:    "key_on_two_actions",
		Message: "one key is bound to two actions a screen answers, so the panel would refuse to draw",
	}
	errCycleKeyUnavailable = &ProblemError{
		Code:    "cycle_key_unavailable",
		Message: "a key that moves between the panel's views and an agent's pane cannot be bound",
	}
	errKeyHeldByYourTmux = &ProblemError{
		Code:    "key_held_by_your_tmux",
		Message: "your own tmux uses keys the panel needs, so they never reach a panel opened inside it",
	}
)

// newKeybindingsCheck asserts tmux is not holding a key the panel reads.
func newKeybindingsCheck() Check {
	return Check{
		Name:        "keybindings",
		Description: "the panel's keys reach it rather than being answered by tmux first",
		Needs:       []string{"config"},
		Run: func(ctx context.Context, env Env) Result {
			cfg, err := env.LoadConfig()
			if err != nil {
				return fail(fmt.Errorf("%w: %w", errConfigUnreadable, err), "", "")
			}

			if env.ValidateKeybindings == nil {
				return fail(fmt.Errorf("%w: nothing was given to validate the keybindings with", errCheckPanicked), "", "")
			}
			if err := env.ValidateKeybindings(cfg.Keybindings); err != nil {
				return fail(
					fmt.Errorf("%w: %w", errKeyOnTwoActions, err),
					"",
					"give one of the two actions a key of its own",
				)
			}

			rootKeys := readRootKeys(ctx, env)

			prefix := readTmuxPrefix(ctx, env)
			for _, key := range xslice.Map(cfg.Keybindings.Cycle.Views, tmux.SpellKey) {
				if prefix == key {
					return fail(
						fmt.Errorf("%w: %s is your tmux prefix, which tmux reads before the root table",
							errCycleKeyUnavailable, key),
						"prefix "+prefix,
						"set-option -g prefix to something other than "+key+", or pick another pair in keybindings.cycle.views",
					)
				}

				if held, ok := heldByAnythingElse(rootKeys, key); ok {
					return fail(
						fmt.Errorf("%w: %s is bound in your tmux root table to %s",
							errCycleKeyUnavailable, key, held),
						key,
						"unbind -n "+key+" in your tmux config",
					)
				}
			}

			var waiting []string

			for _, key := range cfg.Keybindings.GetBoundKeys() {
				spelled := tmux.SpellKey(key)

				held, ok := heldByAnythingElse(rootKeys, spelled)
				if !ok {
					continue
				}

				if !env.IsPanelRunning(ctx) {
					waiting = append(waiting, spelled)

					continue
				}

				return fail(
					fmt.Errorf("%w: %q is %s to tmux, bound in the root table to %s",
						errKeyHeldByTmux, key, spelled, held),
					key,
					"reopen the panel, which takes "+spelled+" back; if it stays with tmux, read the panel log or unbind -n "+spelled+" in your tmux config",
				)
			}

			if len(waiting) > 0 {
				return pass("the panel takes " + strings.Join(waiting, ", ") + " for its own pane when it opens")
			}

			return pass("no panel key is held by tmux")
		},
	}
}

// newYourTmuxKeysCheck asserts the tmux the panel is opened inside lets the panel's keys through.
func newYourTmuxKeysCheck() Check {
	return Check{
		Name:        "your tmux keys",
		Description: "a panel opened inside your own tmux still gets its keys",
		Needs:       []string{"config"},
		Run: func(ctx context.Context, env Env) Result {
			socket := tmux.SocketFromEnv(env.InsideTmux)
			if socket == "" {
				return skip("komrad is not running inside tmux")
			}
			if filepath.Base(socket) == env.TmuxSocket {
				return skip("this is komrad's own tmux")
			}

			cfg, err := env.LoadConfig()
			if err != nil {
				return fail(fmt.Errorf("%w: %w", errConfigUnreadable, err), "", "")
			}

			out, err := env.Output(ctx, "tmux", "-S", socket, "list-keys", "-T", "root")
			if err != nil {
				return skip("your tmux did not list its keys")
			}

			rootKeys := map[string]string{}
			for _, line := range strings.Split(out, "\n") {
				if key, command, ok := parseRootBinding(line); ok {
					rootKeys[key] = command
				}
			}

			var prefixes []string
			for _, option := range []string{"prefix", "prefix2"} {
				prefix, err := env.Output(ctx, "tmux", "-S", socket, "show-options", "-gqv", option)
				if err == nil && strings.TrimSpace(prefix) != "" {
					prefixes = append(prefixes, strings.TrimSpace(prefix))
				}
			}

			var held []string
			for _, key := range slices.Concat(cfg.Keybindings.Cycle.Views, cfg.Keybindings.GetBoundKeys()) {
				spelled := tmux.SpellKey(key)
				if slices.Contains(held, spelled) {
					continue
				}

				if slices.Contains(prefixes, spelled) {
					return Result{
						Status: StatusWarn,
						Detail: "prefix " + spelled,
						Err:    fmt.Errorf("%w: %s is your tmux prefix", errKeyHeldByYourTmux, spelled),
						Fix:    "pick another pair in keybindings.cycle.views, or give your tmux a different prefix",
					}
				}

				command, ok := rootKeys[spelled]
				if ok && !doesPassToNestedTmux(command) {
					held = append(held, spelled)
				}
			}

			if len(held) == 0 {
				return pass("your tmux lets every panel key through")
			}

			return Result{
				Status: StatusWarn,
				Detail: strings.Join(held, ", "),
				Err:    fmt.Errorf("%w: %s", errKeyHeldByYourTmux, strings.Join(held, ", ")),
				Fix:    "change those bindings in your tmux config to send the key on when the pane is running tmux, or remove them with unbind -n",
			}
		},
	}
}

// doesPassToNestedTmux reports whether a root binding checks for a tmux client in the pane before it acts.
func doesPassToNestedTmux(command string) bool {
	return strings.Contains(command, "tmux$")
}

// readRootKeys is what the root table of komrad's own tmux server holds, keyed by the key, and is
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

// heldByAnythingElse reports the command the root table runs for a key, ignoring the bindings
// the panel holds itself.
func heldByAnythingElse(rootKeys map[string]string, key string) (string, bool) {
	command, held := rootKeys[key]
	if !held || isPanelCycleBinding(command) || tmux.IsPanelPassThrough(command) {
		return "", false
	}

	return command, true
}

// isPanelCycleBinding reports whether a root binding is one the panel moves between views with.
func isPanelCycleBinding(command string) bool {
	return strings.HasPrefix(strings.ReplaceAll(command, `"`, ""), "send-keys -t %")
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
