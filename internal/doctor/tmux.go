package doctor

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"komrad/internal/harness"
)

var (
	errTmuxMissing = &ProblemError{
		Code:    "tmux_missing",
		Message: "tmux is not on PATH",
	}
	errTmuxNotRunnable = &ProblemError{
		Code:    "tmux_not_runnable",
		Message: "tmux is on PATH but would not run",
	}
	errTmuxTooOld = &ProblemError{
		Code:    "tmux_too_old",
		Message: "tmux is too old for the panel's key binding",
	}
	errTmuxVersionUnreadable = &ProblemError{
		Code:    "tmux_version_unreadable",
		Message: "tmux did not report a version this understands",
	}
	errTmuxShellUnsupported = &ProblemError{
		Code:    "tmux_shell_unsupported",
		Message: "tmux would start agent panes in a shell that cannot run an agent",
	}
	errTmuxNoServer = &ProblemError{
		Code:    "tmux_no_server",
		Message: "no tmux server is running to read the option from",
	}
)

var minTmux = tmuxVersion{major: 3, minor: 3}

// newTmuxBinaryCheck asserts tmux is installed, without which nothing works.
func newTmuxBinaryCheck() Check {
	return Check{
		Name:        "tmux",
		Description: "tmux runs the panel and every agent pane",
		Run: func(_ context.Context, env Env) Result {
			path, err := env.LookPath("tmux")
			if err != nil {
				return fail(errTmuxMissing, "", "install tmux "+minTmux.String()+" or newer")
			}

			return pass(path)
		},
	}
}

// newTmuxVersionCheck asserts the installed tmux is new enough for the panel, warning
// rather than failing on a version it cannot parse.
func newTmuxVersionCheck() Check {
	return Check{
		Name:        "tmux version",
		Description: "the panel needs tmux " + minTmux.String() + " or newer",
		Needs:       []string{"tmux"},
		Run: func(ctx context.Context, env Env) Result {
			reported, err := env.Output(ctx, "tmux", "-V")
			if err != nil {
				return fail(
					fmt.Errorf("%w: %w", errTmuxNotRunnable, err),
					"",
					"reinstall tmux",
				)
			}

			version, err := parseTmuxVersion(reported)
			if err != nil {
				return warn(fmt.Errorf("%w: %w", errTmuxVersionUnreadable, err), reported)
			}

			if !version.isAtLeast(minTmux) {
				return fail(
					fmt.Errorf("%w: found %s, need %s or newer", errTmuxTooOld, reported, minTmux),
					reported,
					"upgrade tmux to "+minTmux.String()+" or newer",
				)
			}

			return pass(reported)
		},
	}
}

type tmuxVersion struct {
	major int
	minor int
}

func (v tmuxVersion) String() string {
	return strconv.Itoa(v.major) + "." + strconv.Itoa(v.minor)
}

func (v tmuxVersion) isAtLeast(other tmuxVersion) bool {
	if v.major != other.major {
		return v.major > other.major
	}

	return v.minor >= other.minor
}

func parseTmuxVersion(reported string) (tmuxVersion, error) {
	number := strings.TrimSpace(strings.TrimPrefix(reported, "tmux "))
	number = strings.TrimPrefix(number, "next-")

	major, rest, found := strings.Cut(number, ".")
	if !found {
		return tmuxVersion{}, fmt.Errorf("%q is not a major.minor version", reported)
	}

	majorNumber, err := strconv.Atoi(major)
	if err != nil {
		return tmuxVersion{}, fmt.Errorf("%q does not open with a version number", reported)
	}

	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return tmuxVersion{}, fmt.Errorf("%q has no minor version", reported)
	}

	minorNumber, err := strconv.Atoi(rest[:end])
	if err != nil {
		return tmuxVersion{}, fmt.Errorf("%q has no minor version", reported)
	}

	return tmuxVersion{major: majorNumber, minor: minorNumber}, nil
}

// newTmuxShellCheck asserts the shell tmux starts agent panes with can parse the command that
// starts an agent.
func newTmuxShellCheck() Check {
	return Check{
		Name:        "tmux shell",
		Description: "agent panes are started through tmux's default-shell",
		Needs:       []string{"tmux"},
		Run: func(ctx context.Context, env Env) Result {
			shell, from, err := readTmuxDefaultShell(ctx, env)
			if err != nil {
				return warn(err, "")
			}

			if shell == "" {
				return pass("tmux names no default-shell, so panes take the system one")
			}

			if err := harness.RequirePOSIXShell(shell); err != nil {
				return fail(
					fmt.Errorf("%w: %s is not one an agent can be launched from", errTmuxShellUnsupported, shell),
					shell,
					"set-option -g default-shell /bin/sh in your tmux config",
				)
			}

			return pass(shell + " (" + from + ")")
		},
	}
}

func readTmuxDefaultShell(ctx context.Context, env Env) (shell, from string, err error) {
	servers := []struct {
		name string
		args []string
	}{
		{name: "komrad's tmux server", args: []string{"-L", env.TmuxSocket}},
		{name: "your tmux server", args: nil},
	}

	for _, server := range servers {
		if server.args == nil && env.InsideTmux == "" {
			continue
		}
		if server.args != nil && env.TmuxSocket == "" {
			continue
		}

		out, err := env.Output(ctx, "tmux", append(server.args, "show-option", "-gqv", "default-shell")...)
		if err != nil {
			continue
		}

		return out, server.name, nil
	}

	return "", "", fmt.Errorf("%w, so the shell agents would run in is unknown", errTmuxNoServer)
}
