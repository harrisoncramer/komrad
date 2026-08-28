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

var idleInputBox = regexp.MustCompile(`(?m)^─{3,}\s*$\n❯`)

const liveRegionLines = 30

const waitingRegionLines = 6

const idleRegionLines = 6

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// ReadActivity reads what a captured frame says Claude is doing, counting only a frame showing
// the empty input box as idle and anything else unrecognised.
func (h *Harness) ReadActivity(frame string) harness.Activity {
	switch {
	case frame == "":
		return harness.ActivityUnknown
	case doesFrameShowWork(frame):
		return harness.ActivityWorking
	case doesFrameShowWaiting(frame):
		return harness.ActivityWaiting
	case doesFrameShowIdleInputBox(frame):
		return harness.ActivityIdle
	default:
		return harness.ActivityUnrecognized
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

func doesFrameShowIdleInputBox(frame string) bool {
	return idleInputBox.MatchString(tailLines(frame, idleRegionLines))
}
