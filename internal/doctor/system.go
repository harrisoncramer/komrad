package doctor

import (
	"context"
	"strings"
)

const osReleasePath = "/etc/os-release"

// newKomradCheck reports which build the binary running the doctor is.
func newKomradCheck() Check {
	return Check{
		Name:        "komrad",
		Description: "the build of the binary on this machine",
		Run: func(_ context.Context, env Env) Result {
			if env.Build == "" {
				return skip("this binary did not say which build it is")
			}

			return pass(env.Build)
		},
	}
}

// newSystemCheck reports the operating system, its release and the architecture of the
// machine it runs on.
func newSystemCheck() Check {
	return Check{
		Name:        "system",
		Description: "the operating system and architecture of this machine",
		Run: func(ctx context.Context, env Env) Result {
			detail := env.OS + " " + env.Arch

			release := describeRelease(ctx, env)
			if release == "" {
				return pass(detail)
			}

			return pass(detail + ", " + release)
		},
	}
}

func describeRelease(ctx context.Context, env Env) string {
	switch env.OS {
	case "darwin":
		version, err := env.Output(ctx, "sw_vers", "-productVersion")
		if err != nil || version == "" {
			return ""
		}

		return "macOS " + version

	case "linux":
		contents, err := env.ReadFile(osReleasePath)
		if err != nil {
			return ""
		}

		return parsePrettyName(string(contents))
	}

	return ""
}

func parsePrettyName(contents string) string {
	for line := range strings.Lines(contents) {
		value, found := strings.CutPrefix(strings.TrimSpace(line), "PRETTY_NAME=")
		if found {
			return strings.Trim(value, `"'`)
		}
	}

	return ""
}
