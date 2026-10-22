package doctor

import (
	"context"
	"fmt"
	"strings"

	"komrad/config"
	"komrad/internal/harness"
	"komrad/lib/xslice"
)

var (
	errConfigUnreadable = &problemError{
		Code:    "config_unreadable",
		Message: "the config file could not be read",
	}
	errHarnessUnknown = &problemError{
		Code:    "harness_unknown",
		Message: "default_harness names an agent CLI this build does not have",
	}
	errHarnessMissing = &problemError{
		Code:    "harness_missing",
		Message: "the agent CLI is not on PATH",
	}
	errHarnessOutdated = &problemError{
		Code:    "harness_outdated",
		Message: "the agent CLI is older than komrad supports",
	}
	errHarnessVersionUnreadable = &problemError{
		Code:    "harness_version_unreadable",
		Message: "the agent CLI's version could not be read",
	}
)

// newConfigCheck asserts the user's config layers parse and names them in merge order.
func newConfigCheck() Check {
	return Check{
		Name:        CheckConfig,
		Description: "the panel reads its keybindings, defaults and connection from TOML files",
		Run: func(_ context.Context, env Env) Result {
			if _, err := env.LoadConfig(); err != nil {
				return fail(
					fmt.Errorf("%w: %w", errConfigUnreadable, err),
					describeLayers(env),
					describeConfigFix(env),
				)
			}

			return pass(describeLayers(env))
		},
	}
}

func describeLayers(env Env) string {
	if len(env.configLayers) == 0 {
		return "no config files"
	}

	described := make([]string, 0, len(env.configLayers))
	for _, layer := range env.configLayers {
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
	for _, layer := range env.configLayers {
		if env.DoesLayerExist(layer.Path) {
			return "fix or delete the file named above, since every key in it has a default"
		}
	}

	if len(env.configLayers) == 0 {
		return "drop --config, or KOMRAD_CONFIG, to run on the defaults"
	}

	return "write " + env.configLayers[0].Path + ", or drop --config to run on the defaults"
}

// newDefaultHarnessCheck asserts the CLI a new agent runs is one this build knows how to run.
func newDefaultHarnessCheck() Check {
	return Check{
		Name:        CheckDefaultHarness,
		Description: "the agent CLI a new agent runs when nothing names one",
		Needs:       []string{"config"},
		Run: func(_ context.Context, env Env) Result {
			cfg, err := env.LoadConfig()
			if err != nil {
				return fail(fmt.Errorf("%w: %w", errConfigUnreadable, err), "", "")
			}

			for _, candidate := range env.Harnesses {
				if candidate.ID == cfg.DefaultHarness {
					return pass(cfg.DefaultHarness)
				}
			}

			return fail(
				fmt.Errorf("%w: %q, and this build has %s",
					errHarnessUnknown, cfg.DefaultHarness, listNames(env.Harnesses)),
				cfg.DefaultHarness,
				"set default_harness to "+listNames(env.Harnesses),
			)
		},
	}
}

// newHarnessBinaryCheck asserts one agent CLI is on PATH and new enough to launch, a missing one
// leaving the pane idle at a "command not found" the reconciler reads as finished.
func newHarnessBinaryCheck(agent harnessInfo) Check {
	return Check{
		Name:        agent.ID,
		Description: "agents on the " + agent.ID + " harness run this command",
		Run: func(ctx context.Context, env Env) Result {
			isDefault := effectiveDefaultHarness(env) == agent.ID

			path, lookErr := env.LookPath(agent.Binary)
			if lookErr != nil {
				if !isDefault {
					return warn(
						fmt.Errorf("%w: %s, so only agents set to this harness are affected",
							errHarnessMissing, agent.Binary),
						"not on PATH",
					)
				}

				return fail(
					fmt.Errorf("%w: %s, which is what new agents run", errHarnessMissing, agent.Binary),
					"not on PATH",
					"install "+agent.Binary+", or point default_harness at a CLI you have",
				)
			}

			reported := readHarnessVersion(ctx, env, path)
			if reported == "" {
				return pass(path)
			}

			detail := path + ", " + reported
			if agent.MinVersion == "" {
				return pass(detail)
			}

			found, older, err := harness.ReadVersion(reported, agent.MinVersion)
			if err != nil {
				return warn(fmt.Errorf("%w: %w", errHarnessVersionUnreadable, err), detail)
			}
			if !older {
				return pass(detail)
			}

			if !isDefault {
				return warn(
					fmt.Errorf("%w: found %s, need %s or newer, so agents set to this harness fail to launch",
						errHarnessOutdated, found, agent.MinVersion),
					detail,
				)
			}

			return fail(
				fmt.Errorf("%w: found %s, need %s or newer, and new agents run it", errHarnessOutdated, found, agent.MinVersion),
				detail,
				"upgrade "+agent.Binary+" to "+agent.MinVersion+" or newer",
			)
		},
	}
}

func readHarnessVersion(ctx context.Context, env Env, path string) string {
	reported, err := env.Output(ctx, path, "--version")
	if err != nil {
		return ""
	}

	first, _, _ := strings.Cut(reported, "\n")

	return first
}

func effectiveDefaultHarness(env Env) string {
	cfg, err := env.LoadConfig()
	if err != nil {
		return config.DefaultHarness
	}

	return cfg.DefaultHarness
}

func listNames(harnesses []harnessInfo) string {
	return joinWords(xslice.Map(harnesses, func(agent harnessInfo) string { return agent.ID }))
}
