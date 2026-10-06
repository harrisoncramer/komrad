package doctor

import (
	"context"
	"strings"
)

const osRelease = "/etc/os-release"

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

			release := readOSRelease(ctx, env)
			if release == "" {
				return pass(detail)
			}

			return pass(detail + ", " + release)
		},
	}
}

// newTerminalCheck reports the terminal the panel is drawn in, asking tmux when it stands
// between the two, since tmux names itself to what it runs.
func newTerminalCheck() Check {
	return Check{
		Name:        "terminal",
		Description: "the terminal the panel is drawn in",
		Run: func(ctx context.Context, env Env) Result {
			terminal := findTerminal(ctx, env)
			if terminal == "" {
				return skip("the terminal did not name itself")
			}

			return pass(terminal)
		},
	}
}

func readOSRelease(ctx context.Context, env Env) string {
	switch env.OS {
	case "darwin":
		version, err := env.Output(ctx, "sw_vers", "-productVersion")
		if err != nil || version == "" {
			return ""
		}

		return "macOS " + version

	case "linux":
		contents, err := env.ReadFile(osRelease)
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

func findTerminal(ctx context.Context, env Env) string {
	if env.InsideTmux == "" {
		return strings.TrimSpace(env.TermProgram + " " + env.TermProgramVersion)
	}

	named, err := env.Output(ctx, "tmux", "display-message", "-p", "#{client_termtype}\t#{client_termname}")
	if err != nil {
		return ""
	}

	termType, termName, _ := strings.Cut(named, "\t")
	if strings.TrimSpace(termType) != "" {
		return strings.TrimSpace(termType)
	}

	return strings.TrimSpace(termName)
}
