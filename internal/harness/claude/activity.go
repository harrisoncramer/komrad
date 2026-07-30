package claude

import (
	"regexp"
	"strings"

	"claude-agents/internal/harness"
)

// workingLine matches Claude Code's live status line, shown only while it is busy
// generating or running a tool. The shape is a spinner glyph, a space, a verb, and a
// ellipsis, like "✽ Beboppin'…" or "· Cooking… (1m 50s · ↓ 5.4k tokens)". The
// trailing timer appears only once tokens start streaming, so keying on it would leave
// every no-timer frame undetected. The glyph is never a letter, so requiring a
// non-letter lead excludes prose that merely ends a word with an ellipsis, and
// excluding the tool-result "⎿", the "❯" prompt, and box rules keeps that chrome from
// matching. workingTimer catches the parenthesised elapsed timer on its own.
var (
	workingLine  = regexp.MustCompile(`(?m)^\s*[^\s\p{L}⎿❯│─╭╮╰╯-]\s+\p{L}.*…`)
	workingTimer = regexp.MustCompile(`…\s*\(\s*\d+m?\s*\d*\s*s`)
)

// legacyWorkingMarker is the wording older builds put on that same line instead
// of a token count. Accept it too so the detector spans releases.
const legacyWorkingMarker = "esc to interrupt"

// waitingFooter matches the footer chrome Claude renders under any interactive
// selection it is blocked on, which reads "Enter to select · ↑/↓ to navigate · Esc to
// cancel". Those phrases are wording-independent, distinct from the working line's "esc
// to interrupt", and absent from the plain input box a finished agent sits at. Keying on
// the footer rather than the prompt's own prose keeps transcript text from being
// mistaken for a live prompt.
var waitingFooter = regexp.MustCompile(`(?i)esc to cancel|enter to select`)

// liveRegionLines is how many lines from the bottom of a captured frame count as the
// live UI. It has to be wide enough to hold the working status line even when a plan or
// todo list renders under it, pushing it well up from the bottom.
const liveRegionLines = 30

// waitingRegionLines is a much tighter window for the waiting check, since the
// selection footer replaces the input box and is the bottom-most line while a prompt is
// up. Keeping it tight matters because the footer's phrases can appear in ordinary
// transcript text, which the wide working window would read as a live prompt.
const waitingRegionLines = 6

// ansiSeq matches an ANSI escape sequence. The pane is captured with colors via -e, so
// the status line comes back shredded by SGR codes, the verb split across color runs
// and codes wedged between the ellipsis and the timer.
var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// Activity reads what a captured frame says Claude is doing. Anything that is
// neither working nor a live prompt is idle rather than unknown: an agent sitting
// at its input box is the ordinary finished case, and reporting no verdict there
// would leave every agent stuck in the state it last held.
func (h *Harness) Activity(frame string) harness.Activity {
	switch {
	case frame == "":
		return harness.ActivityUnknown
	case frameShowsWork(frame):
		return harness.ActivityWorking
	case frameShowsWaiting(frame):
		return harness.ActivityWaiting
	default:
		return harness.ActivityIdle
	}
}

// tailLines returns the last n lines of a frame with ANSI escapes stripped, the region
// where the live UI sits. Transcript prose scrolls above it and can contain any marker
// text, so detection stays inside this window.
func tailLines(frame string, n int) string {
	frame = ansiSeq.ReplaceAllString(frame, "")
	lines := strings.Split(strings.TrimRight(frame, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}

	return strings.Join(lines, "\n")
}

// frameShowsWork reports whether the frame's live region shows Claude actively
// working, as opposed to sitting idle at its input prompt.
func frameShowsWork(frame string) bool {
	region := tailLines(frame, liveRegionLines)
	return workingLine.MatchString(region) || workingTimer.MatchString(region) || strings.Contains(region, legacyWorkingMarker)
}

// frameShowsWaiting reports whether the frame's live region shows Claude blocked on the
// user at an interactive prompt.
func frameShowsWaiting(frame string) bool {
	return waitingFooter.MatchString(tailLines(frame, waitingRegionLines))
}
