package doctor

import (
	"context"
	"fmt"

	"weave/internal/appconfig"
)

// The ways the config can be wrong.
var (
	ErrConfigUnreadable = &Problem{
		Code:    "config_unreadable",
		Message: "the config file could not be read",
	}
	ErrHarnessUnknown = &Problem{
		Code:    "harness_unknown",
		Message: "default_harness names an agent CLI this build does not have",
	}
	ErrHarnessMissing = &Problem{
		Code:    "harness_missing",
		Message: "the agent CLI is not on PATH",
	}
)

// Config asserts the user's config file parses. An unknown key is an error rather than a
// dropped line, so a typo takes the whole panel down to an error screen. Having no file is
// the ordinary install and passes.
func Config() Check {
	return Check{
		Name:        "config",
		Description: "the panel reads its keybindings and defaults from a TOML file",
		Run: func(_ context.Context, env Env) Result {
			if _, err := env.LoadConfig(); err != nil {
				return fail(
					fmt.Errorf("%w: %w", ErrConfigUnreadable, err),
					env.ConfigPath,
					"fix or delete "+env.ConfigPath+", since every key in it has a default",
				)
			}

			// Whether the file is there is not worth its own status. Both answers
			// are correct installs, and which one this is only matters when a
			// setting is not taking effect, so it is said rather than judged.
			if !env.DoesConfigExist() {
				return pass("no file at " + env.ConfigPath + ", so every default applies")
			}

			return pass(env.ConfigPath)
		},
	}
}

// DefaultHarness asserts the CLI a new agent runs is one this build knows how to run. The
// config takes a name rather than a choice, so a misspelling is otherwise only found when
// somebody tries to create an agent.
func DefaultHarness() Check {
	return Check{
		Name:        "default harness",
		Description: "the agent CLI a new agent runs when nothing names one",
		Needs:       []string{"config"},
		Run: func(_ context.Context, env Env) Result {
			cfg, err := env.LoadConfig()
			if err != nil {
				return fail(fmt.Errorf("%w: %w", ErrConfigUnreadable, err), "", "")
			}

			for _, candidate := range env.Harnesses {
				if candidate.ID == cfg.DefaultHarness {
					return pass(cfg.DefaultHarness)
				}
			}

			return fail(
				fmt.Errorf("%w: %q, and this build has %s",
					ErrHarnessUnknown, cfg.DefaultHarness, listNames(env.Harnesses)),
				cfg.DefaultHarness,
				"set default_harness to "+listNames(env.Harnesses),
			)
		},
	}
}

// HarnessBinary asserts one agent CLI is on PATH, which is the quietest way weave breaks.
// The launch is a line typed into a pane, so a CLI that is not there leaves the pane
// printing "command not found" and going idle, and the reconciler reads idle as an agent
// that finished.
//
// Only the CLI new agents actually run is a failure. Weave supports several and nobody
// installs all of them.
func HarnessBinary(agent HarnessInfo) Check {
	return Check{
		Name:        agent.ID,
		Description: "agents on the " + agent.ID + " harness run this command",
		Run: func(_ context.Context, env Env) Result {
			path, lookErr := env.LookPath(agent.Binary)
			if lookErr == nil {
				return pass(path)
			}

			if effectiveDefaultHarness(env) != agent.ID {
				return warn(
					fmt.Errorf("%w: %s, so only agents set to this harness are affected",
						ErrHarnessMissing, agent.Binary),
					"not on PATH",
				)
			}

			return fail(
				fmt.Errorf("%w: %s, which is what new agents run", ErrHarnessMissing, agent.Binary),
				"not on PATH",
				"install "+agent.Binary+", or point default_harness at a CLI you have",
			)
		},
	}
}

// effectiveDefaultHarness is the CLI a new agent would actually run. A config that will not
// parse falls back to the built-in default, which is what the root command does too, so
// this check still answers when the config is broken.
func effectiveDefaultHarness(env Env) string {
	cfg, err := env.LoadConfig()
	if err != nil {
		return appconfig.DefaultHarness
	}

	return cfg.DefaultHarness
}

// listNames is the registered harness ids as a sentence fragment.
func listNames(harnesses []HarnessInfo) string {
	names := make([]string, 0, len(harnesses))
	for _, agent := range harnesses {
		names = append(names, agent.ID)
	}

	return joinWords(names)
}
