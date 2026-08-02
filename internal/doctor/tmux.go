package doctor

import (
	"context"
	"fmt"
	"strconv"
	"strings"
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
