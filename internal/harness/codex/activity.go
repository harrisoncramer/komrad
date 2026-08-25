package codex

import (
	"regexp"
	"slices"
	"strings"

	"weave/internal/harness"
)

var workingLine = regexp.MustCompile(`(?m)^\s*[^\s\p{L}]?\s*Working\s*\(\s*\d+`)

const interruptHint = "esc to interrupt"

var waitingFooter = regexp.MustCompile(`(?i)press enter to (confirm|continue)|enter to submit answer`)

const liveRegionLines = 30

const waitingRegionLines = 8

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// ReadActivity reads what a captured frame says Codex is doing. Anything neither
// working nor blocked on the user is idle rather than unknown, which is also where an
// interrupted turn lands: the live line vanishes and the typed prompt stays in the
// input box, so an escaped agent reads as finished.
//
// The footer of a live prompt is read first because the question Codex asks with its
// own tool keeps the turn open, and that footer offers to interrupt the turn alongside
// the key that answers it. Read any later and the interrupt hint wins, leaving an agent
// blocked on a person looking like one that is still thinking. Only the bottom line
// counts for that, since it is the one place a frame cannot be quoting itself, and the
// looser search over the region above stays behind work for the same reason.
func (h *Harness) ReadActivity(frame string) harness.Activity {
	switch {
	case frame == "":
		return harness.ActivityUnknown
	case doesFrameShowPromptFooter(frame):
		return harness.ActivityWaiting
	case doesFrameShowWork(frame):
		return harness.ActivityWorking
	case doesFrameShowWaiting(frame):
		return harness.ActivityWaiting
	default:
		return harness.ActivityIdle
	}
}

func tailLines(frame string, n int) string {
	frame = ansiSeq.ReplaceAllString(frame, "")
	lines := strings.Split(strings.TrimRight(frame, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}

	return strings.Join(lines, "\n")
}

func doesFrameShowWork(frame string) bool {
	region := tailLines(frame, liveRegionLines)

	return workingLine.MatchString(region) || strings.Contains(region, interruptHint)
}

func doesFrameShowWaiting(frame string) bool {
	return waitingFooter.MatchString(tailLines(frame, waitingRegionLines))
}

func doesFrameShowPromptFooter(frame string) bool {
	return waitingFooter.MatchString(lastFilledLine(frame))
}

// lastFilledLine is the bottom-most line with anything on it, which is where Codex
// draws the footer of whatever it is asking. Everything a turn produces has the input
// box under it, so a phrase in the transcript never reaches here.
func lastFilledLine(frame string) string {
	lines := strings.Split(ansiSeq.ReplaceAllString(frame, ""), "\n")
	for _, line := range slices.Backward(lines) {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}

	return ""
}
