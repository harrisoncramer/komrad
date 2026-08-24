package claude

import (
	"regexp"
	"strings"

	"weave/internal/harness"
)

var (
	workingLine  = regexp.MustCompile(`(?m)^\s*[^\s\p{L}⎿❯│─╭╮╰╯-]\s+\p{L}.*…`)
	workingTimer = regexp.MustCompile(`…\s*\(\s*\d+m?\s*\d*\s*s`)
)

var waitingFooter = regexp.MustCompile(`(?i)esc to cancel|enter to select`)

const liveRegionLines = 30

const waitingRegionLines = 6

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// ReadActivity reads what a captured frame says Claude is doing. Anything neither
// working nor a live prompt is idle rather than unknown. An agent sitting at its
// input box is the ordinary finished case, and reporting no verdict there would
// leave every agent stuck in the state it last held.
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
	return workingLine.MatchString(region) || workingTimer.MatchString(region)
}

func doesFrameShowWaiting(frame string) bool {
	return waitingFooter.MatchString(tailLines(frame, waitingRegionLines))
}
