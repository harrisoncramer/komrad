package codex

import (
	"regexp"
	"strings"

	"weave/internal/harness"
)

// workingLine matches Codex's live status line, shown only while it is generating or
// running a tool. It reads "• Working (5s • esc to interrupt)", with the timer present
// from the first frame rather than appearing once tokens stream.
//
// The bullet cannot carry the match on its own. Codex prefixes its own prose and every
// tool line with the same glyph, so "• Running ls" and "• I'll run that command" are
// both ordinary transcript text. The word and the timer together are what only the
// live line has.
var workingLine = regexp.MustCompile(`(?m)^\s*[^\s\p{L}]?\s*Working\s*\(\s*\d+`)

// interruptHint is the other half of that line. It is matched on its own so a release
// that drops the timer or renames the verb still reads as working, since nothing but
// the live line offers a way to interrupt.
const interruptHint = "esc to interrupt"

// waitingFooter matches the footer under anything Codex is blocked on. An approval
// reads "Press enter to confirm or esc to cancel", while the directory-trust question
// and the sign-in screen read "Press enter to continue". All three are the user's turn,
// and the wording is stable across the numbered choices above it.
//
// It is deliberately not keyed on the "›" caret that marks the selected choice, since
// that same glyph marks the ordinary input box.
var waitingFooter = regexp.MustCompile(`(?i)press enter to (confirm|continue)`)

// liveRegionLines is how many lines from the bottom of a captured frame count as the
// live UI. It has to be wide enough to hold the working line even when a tool block or
// a diff renders under it, pushing it well up from the bottom.
const liveRegionLines = 30

// waitingRegionLines is a tighter window for the waiting check, since the footer is
// the bottom-most line whenever Codex is blocked. Keeping it tight matters because the
// same phrase can appear in transcript prose, which the wide working window would read
// as a live prompt.
const waitingRegionLines = 8

// ansiSeq matches an ANSI escape sequence. The pane is captured with colors via -e, so
// the status line comes back shredded by SGR codes.
var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// ReadActivity reads what a captured frame says Codex is doing. Anything neither
// working nor blocked on the user is idle rather than unknown, which is also where an
// interrupted turn lands: the live line vanishes and the typed prompt stays in the
// input box, so an escaped agent reads as finished.
//
// The frame is all there is. Codex keeps no account of what a running session is doing:
// its hooks cover a turn that ends on its own but not one the user interrupts, and its
// state database records a conversation without recording whether it is mid-turn. So
// every verdict here is inferred and none is reported.
func (h *Harness) ReadActivity(obs harness.Observation) harness.Reading {
	return harness.Reading{Activity: readFrame(obs.Frame)}
}

// readFrame reads what a captured frame says Codex is doing.
func readFrame(frame string) harness.Activity {
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

// frameShowsWork reports whether the frame's live region shows Codex working, as
// opposed to sitting at its input box.
func frameShowsWork(frame string) bool {
	region := tailLines(frame, liveRegionLines)

	return workingLine.MatchString(region) || strings.Contains(region, interruptHint)
}

// frameShowsWaiting reports whether the frame's live region shows Codex blocked on the
// user, at an approval, the directory-trust question, or the sign-in screen.
func frameShowsWaiting(frame string) bool {
	return waitingFooter.MatchString(tailLines(frame, waitingRegionLines))
}
