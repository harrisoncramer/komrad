package codex

import (
	"regexp"
	"strings"

	"weave/internal/harness"
)

var workingLine = regexp.MustCompile(`(?m)^\s*[^\s\p{L}]?\s*Working\s*\(\s*\d+`)

const interruptHint = "esc to interrupt"

var waitingFooter = regexp.MustCompile(`(?i)press enter to (confirm|continue)`)

const liveRegionLines = 30

const waitingRegionLines = 8

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// ReadActivity reads what a captured frame says Codex is doing. Anything neither
// working nor blocked on the user is idle rather than unknown, which is also where an
// interrupted turn lands: the live line vanishes and the typed prompt stays in the
// input box, so an escaped agent reads as finished.
func (h *Harness) ReadActivity(frame string) harness.Activity {
	switch {
	case frame == "":
		return harness.ActivityUnknown
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
