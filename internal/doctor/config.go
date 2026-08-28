package doctor

import (
	"context"
	"fmt"
	"strings"

	"weave/config"
	"weave/lib/xslice"
)

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

// Config asserts the user's config layers parse and names them in merge order, since with
// more than one file the loader is the only thing that knows where a value came from.
func Config() Check {
	return Check{
		Name:        "config",
		Description: "the panel reads its keybindings, defaults and connection from TOML files",
		Run: func(_ context.Context, env Env) Result {
			if _, err := env.LoadConfig(); err != nil {
				return fail(
					fmt.Errorf("%w: %w", ErrConfigUnreadable, err),
					describeLayers(env),
					describeConfigFix(env),
				)
			}

			return pass(describeLayers(env))
		},
	}
}

func describeLayers(env Env) string {
	if len(env.ConfigLayers) == 0 {
		return "no config files"
	}

	described := make([]string, 0, len(env.ConfigLayers))
	for _, layer := range env.ConfigLayers {
		if env.DoesLayerExist(layer.Path) {
			described = append(described, layer.Path)

			continue
		}

		described = append(described, layer.Path+" (absent)")
	}

	return strings.Join(described, ", ")
}

func describeSource(env Env, key string) string {
	source, ok := env.Sources[key]
	if !ok {
		return ""
	}

	return " from the " + string(source)
}

func describeConfigFix(env Env) string {
	for _, layer := range env.ConfigLayers {
		if env.DoesLayerExist(layer.Path) {
			return "fix or delete the file named above, since every key in it has a default"
		}
	}

	if len(env.ConfigLayers) == 0 {
		return "drop --config, or WEAVE_CONFIG, to run on the defaults"
	}

	return "write " + env.ConfigLayers[0].Path + ", or drop --config to run on the defaults"
}

// DefaultHarness asserts the CLI a new agent runs is one this build knows how to run; the
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

// HarnessBinary asserts one agent CLI is on PATH, since a missing one leaves the pane
// printing "command not found" and going idle, which the reconciler reads as finished.
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

func effectiveDefaultHarness(env Env) string {
	cfg, err := env.LoadConfig()
	if err != nil {
		return config.DefaultHarness
	}

	return cfg.DefaultHarness
}

func listNames(harnesses []HarnessInfo) string {
	return joinWords(xslice.Map(harnesses, func(agent HarnessInfo) string { return agent.ID }))
}
