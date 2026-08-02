package doctor

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"weave/internal/harness"
)

// minTmux is the oldest tmux the panel's key binding works on. display-popup arrived in
// 3.2, but weave.tmux passes it -T for the popup title, which arrived in 3.3.
var minTmux = tmuxVersion{major: 3, minor: 3}

// The ways tmux can be wrong. Each is a code a script can switch on, so the wording here
// can change without breaking a caller.
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

// TmuxBinary asserts tmux is installed. Nothing about the panel works without it: the
// popup, the agent panes, and the manager server are all tmux.
func TmuxBinary() Check {
	return Check{
		Name:        "tmux",
		Description: "tmux runs the panel popup and every agent pane",
		Run: func(_ context.Context, env Env) Result {
			path, err := env.LookPath("tmux")
			if err != nil {
				// What LookPath returned is dropped rather than wrapped: it only
				// ever says the name was not found, which the sentinel says
				// better, and it names PATH in a way that reads as the cause.
				return fail(ErrTmuxMissing, "", "install tmux "+minTmux.String()+" or newer")
			}

			return ok(path)
		},
	}
}

// TmuxVersion asserts the installed tmux is new enough for the key binding.
//
// A version that will not parse is a warning rather than a failure. tmux forks spell it
// their own way, OpenBSD's reports the OS release instead, and refusing to start against
// one of those is worse than being wrong about a version that probably works.
func TmuxVersion() Check {
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

			if !version.atLeast(minTmux) {
				return fail(
					fmt.Errorf("%w: found %s, need %s or newer", ErrTmuxTooOld, reported, minTmux),
					reported,
					"upgrade tmux to "+minTmux.String()+" or newer",
				)
			}

			return ok(reported)
		},
	}
}

// tmuxVersion is a tmux release. The letter tmux appends to a point release is dropped,
// since one has never added or removed a command.
type tmuxVersion struct {
	major int
	minor int
}

func (v tmuxVersion) String() string { return strconv.Itoa(v.major) + "." + strconv.Itoa(v.minor) }

// atLeast reports whether v is other or newer.
func (v tmuxVersion) atLeast(other tmuxVersion) bool {
	if v.major != other.major {
		return v.major > other.major
	}

	return v.minor >= other.minor
}

// parseTmuxVersion reads what "tmux -V" printed.
//
// Three spellings have to survive it: a release, "tmux 3.3a", where the trailing letter is
// not part of the number; a build from master, "tmux next-3.8", which is the release it is
// heading for; and OpenBSD's base tmux, "tmux openbsd-7.6", which reports the OS release
// and says nothing about tmux, so it is refused rather than read as version 7.
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

	// The minor runs until anything that is not a digit, which is where the point
	// release's letter or a suffix such as "-rc" starts.
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

// TmuxShell asserts that the shell tmux would start an agent's pane with can parse the
// command that starts one.
//
// This is the failure that reads as something else. The launch is a string typed into a
// pane, and a shell that cannot parse a command substitution leaves the agent as a syntax
// error next to a prompt file that was never cleaned up. The service refuses to start at
// all rather than let it happen, so what a user sees today is a panel that will not open.
//
// The option is read from a tmux server that is already running rather than from one
// started to ask, since doctor starting a server would be doctor changing the machine it
// is describing. Weave's own server is asked first because that is the one agent panes are
// opened on; the user's is the fallback, and answers the same unless the two servers were
// given different configuration.
func TmuxShell() Check {
	return Check{
		Name:        "tmux shell",
		Description: "agent panes are started through tmux's default-shell",
		Needs:       []string{"tmux"},
		Run: func(ctx context.Context, env Env) Result {
			shell, from, err := tmuxDefaultShell(ctx, env)
			if err != nil {
				return warn(err, "")
			}

			// tmux declining to name one is not evidence of a bad shell, which is
			// the same reading RequirePOSIXShell takes.
			if shell == "" {
				return ok("tmux names no default-shell, so panes take the system one")
			}

			if err := harness.RequirePOSIXShell(shell); err != nil {
				return fail(
					fmt.Errorf("%w: %s is not one an agent can be launched from", ErrTmuxShellUnsupported, shell),
					shell,
					"set-option -g default-shell /bin/sh in your tmux config",
				)
			}

			return ok(shell + " (" + from + ")")
		},
	}
}

// tmuxDefaultShell reads default-shell off whichever tmux server is already up, and says
// which one answered. It reports a problem rather than an empty answer when none is
// running, since "no server to ask" and "the option is unset" are different facts.
func tmuxDefaultShell(ctx context.Context, env Env) (shell, from string, err error) {
	servers := []struct {
		name string
		args []string
	}{
		{name: "weave's tmux server", args: []string{"-L", env.TmuxSocket}},
		{name: "your tmux server", args: nil},
	}

	for _, server := range servers {
		// The user's server is only asked when this is running inside it, since
		// addressing the default socket would start one that was not there.
		if server.args == nil && env.InsideTmux == "" {
			continue
		}
		if server.args != nil && env.TmuxSocket == "" {
			continue
		}

		// show-option before anything that could create a server, and -q so an
		// option that is simply unset comes back empty rather than as an error.
		out, err := env.Output(ctx, "tmux", append(server.args, "show-option", "-gqv", "default-shell")...)
		if err != nil {
			continue
		}

		return out, server.name, nil
	}

	return "", "", fmt.Errorf("%w, so the shell agents would run in is unknown", ErrTmuxNoServer)
}
