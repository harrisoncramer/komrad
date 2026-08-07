package doctor

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"weave/internal/harness"
)

// The ways tmux can be wrong, each its own code so the wording can change without breaking
// a caller that switches on it.
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

// minTmux is the oldest tmux the panel's key binding works on: display-popup arrived in
// 3.2, but the binding passes it -T for the popup title, which arrived in 3.3.
var minTmux = tmuxVersion{major: 3, minor: 3}

// TmuxBinary asserts tmux is installed; the popup, the agent panes, and the manager server
// are all tmux, so nothing works without it.
func TmuxBinary() Check {
	return Check{
		Name:        "tmux",
		Description: "tmux runs the panel popup and every agent pane",
		Run: func(_ context.Context, env Env) Result {
			path, err := env.LookPath("tmux")
			if err != nil {
				// LookPath's error only restates the name, which the sentinel says
				// better, so it is dropped rather than wrapped.
				return fail(ErrTmuxMissing, "", "install tmux "+minTmux.String()+" or newer")
			}

			return pass(path)
		},
	}
}

// TmuxVersion asserts the installed tmux is new enough for the key binding. A version that
// will not parse is a warning rather than a failure, since tmux forks spell it their own
// way and OpenBSD's reports the OS release instead, and refusing to start over one of those
// is worse than being wrong about a version that probably works.
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

// tmuxVersion is a tmux release; the letter tmux appends to a point release is dropped,
// since one has never added or removed a command.
type tmuxVersion struct {
	major int
	minor int
}

func (v tmuxVersion) String() string { return strconv.Itoa(v.major) + "." + strconv.Itoa(v.minor) }

// isAtLeast reports whether v is other or newer.
func (v tmuxVersion) isAtLeast(other tmuxVersion) bool {
	if v.major != other.major {
		return v.major > other.major
	}

	return v.minor >= other.minor
}

// parseTmuxVersion reads what "tmux -V" printed. Three spellings have to survive it. A
// release is "tmux 3.3a", where the trailing letter is not part of the number. A build from
// master is "tmux next-3.8", named for the release it heads for. OpenBSD's base tmux is
// "tmux openbsd-7.6", which reports the OS release and nothing about tmux, so it is refused
// rather than read as version 7.
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

// TmuxShell asserts the shell tmux starts agent panes with can parse the command that
// starts an agent; a shell that cannot leaves the agent as a syntax error, which the service
// refuses to start over, so what a user sees is a panel that will not open. The option is
// read off a server already running rather than one started to ask, since starting a server
// would be doctor changing the machine it is describing.
func TmuxShell() Check {
	return Check{
		Name:        "tmux shell",
		Description: "agent panes are started through tmux's default-shell",
		Needs:       []string{"tmux"},
		Run: func(ctx context.Context, env Env) Result {
			shell, from, err := readTmuxDefaultShell(ctx, env)
			if err != nil {
				return warn(err, "")
			}

			// tmux declining to name one is not evidence of a bad shell, which is
			// the reading RequirePOSIXShell takes too.
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

// readTmuxDefaultShell reads default-shell off whichever tmux server is already up, and
// says which answered; no server to ask and an unset option are different facts, so the
// first is a problem rather than an empty answer. Weave's own server goes first since that
// is where agent panes open, and the user's is the fallback.
func readTmuxDefaultShell(ctx context.Context, env Env) (shell, from string, err error) {
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

		// -q so an option that is unset comes back empty rather than as an error.
		out, err := env.Output(ctx, "tmux", append(server.args, "show-option", "-gqv", "default-shell")...)
		if err != nil {
			continue
		}

		return out, server.name, nil
	}

	return "", "", fmt.Errorf("%w, so the shell agents would run in is unknown", ErrTmuxNoServer)
}
