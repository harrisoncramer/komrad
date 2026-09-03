package doctor

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"weave/internal/harness"
)

var (
	ErrTmuxMissing = &Problem{
		Code:    "tmux_missing",
		Message: "tmux is not on PATH",
	}
	ErrTmuxNotRunnable = &Problem{
		Code:    "tmux_not_runnable",
		Message: "tmux is on PATH but would not run",
	}
	ErrTmuxTooOld = &Problem{
		Code:    "tmux_too_old",
		Message: "tmux is too old for the panel's key binding",
	}
	ErrTmuxVersionUnreadable = &Problem{
		Code:    "tmux_version_unreadable",
		Message: "tmux did not report a version this understands",
	}
	ErrTmuxShellUnsupported = &Problem{
		Code:    "tmux_shell_unsupported",
		Message: "tmux would start agent panes in a shell that cannot run an agent",
	}
	ErrTmuxNoServer = &Problem{
		Code:    "tmux_no_server",
		Message: "no tmux server is running to read the option from",
	}
)

var minTmux = tmuxVersion{major: 3, minor: 3}

// newTmuxBinaryCheck asserts tmux is installed; the popup, the agent panes, and the manager server
// are all tmux, so nothing works without it.
func newTmuxBinaryCheck() Check {
	return Check{
		Name:        "tmux",
		Description: "tmux runs the panel popup and every agent pane",
		Run: func(_ context.Context, env Env) Result {
			path, err := env.LookPath("tmux")
			if err != nil {
				return fail(ErrTmuxMissing, "", "install tmux "+minTmux.String()+" or newer")
			}

			return pass(path)
		},
	}
}

// newTmuxVersionCheck asserts the installed tmux is new enough for the key binding, warning rather
// than failing on a version it cannot parse since forks and OpenBSD spell it their own way.
func newTmuxVersionCheck() Check {
	return Check{
		Name:        "tmux version",
		Description: "the panel's popup needs tmux " + minTmux.String() + " or newer",
		Needs:       []string{"tmux"},
		Run: func(ctx context.Context, env Env) Result {
			reported, err := env.Output(ctx, "tmux", "-V")
			if err != nil {
				return fail(
					fmt.Errorf("%w: %w", ErrTmuxNotRunnable, err),
					"",
					"reinstall tmux",
				)
			}

			version, err := parseTmuxVersion(reported)
			if err != nil {
				return warn(fmt.Errorf("%w: %w", ErrTmuxVersionUnreadable, err), reported)
			}

			if !version.isAtLeast(minTmux) {
				return fail(
					fmt.Errorf("%w: found %s, need %s or newer", ErrTmuxTooOld, reported, minTmux),
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

func (v tmuxVersion) String() string { return strconv.Itoa(v.major) + "." + strconv.Itoa(v.minor) }

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

// newTmuxShellCheck asserts the shell tmux starts agent panes with can parse the command that starts
// an agent, which otherwise fails as a syntax error and leaves the panel refusing to open.
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
					fmt.Errorf("%w: %s is not one an agent can be launched from", ErrTmuxShellUnsupported, shell),
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
		{name: "weave's tmux server", args: []string{"-L", env.TmuxSocket}},
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

	return "", "", fmt.Errorf("%w, so the shell agents would run in is unknown", ErrTmuxNoServer)
}
