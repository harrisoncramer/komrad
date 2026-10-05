// Package checklog cleans the log of a check's Actions job and maps the check run's steps onto its lines.
package checklog

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	errorMarker = "##[error]"
	groupMarker = "##[group]"
	runGroup    = groupMarker + "Run "
	byteOrder   = "\ufeff"
)

// Step is one step of a check run as the forge reported it.
type Step struct {
	Number     int
	Name       string
	Conclusion string
	// StartedAt is nil for a step that never started, which is left off the log.
	StartedAt *time.Time
}

// StepRange is where one step sits in a log.
type StepRange struct {
	Number     int
	Name       string
	Conclusion string
	// FirstLine is the line the step starts on, counting from one.
	FirstLine  int
	LineCount  int
	ErrorCount int
}

// log is a job's log cleaned of timestamps and escape codes, with its steps mapped onto its lines.
type log struct {
	// Text holds every line followed by a newline.
	Text       string
	LineCount  int
	ErrorCount int
	// Steps runs in the order the steps ran, and lines before the first step belong to none.
	Steps []StepRange
}

type line struct {
	at    time.Time
	error bool
	// group is the text after the group marker of a line that opens a run step's group, and empty otherwise.
	group string
}

// Parse reads a job's raw log and cleans it, mapping steps onto its lines by when each step started.
func Parse(r io.Reader, steps []Step) (*log, error) {
	reader := bufio.NewReader(r)
	var text strings.Builder
	var lines []line
	var at time.Time
	errorCount := 0

	for {
		raw, err := reader.ReadString('\n')
		if raw != "" {
			if len(lines) == 0 {
				raw = strings.TrimPrefix(raw, byteOrder)
			}

			cleaned, stamp, stamped := clean(strings.TrimSuffix(raw, "\n"))
			if stamped {
				at = stamp
			}

			l := line{at: at, error: strings.HasPrefix(cleaned, errorMarker)}
			if strings.HasPrefix(cleaned, runGroup) {
				l.group = strings.TrimPrefix(cleaned, groupMarker)
			}
			if l.error {
				errorCount++
			}

			lines = append(lines, l)
			text.WriteString(cleaned)
			text.WriteByte('\n')
		}

		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read the log: %w", err)
		}
	}

	return &log{
		Text:       text.String(),
		LineCount:  len(lines),
		ErrorCount: errorCount,
		Steps:      mapSteps(lines, steps),
	}, nil
}

// Lines splits a log's text back into its lines.
func Lines(text string) []string {
	if text == "" {
		return nil
	}

	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// clean strips a raw line's timestamp, keeps what follows its last carriage return, and removes its escape
// codes.
func clean(raw string) (string, time.Time, bool) {
	stamp, rest, stamped := cutStamp(raw)
	rest = strings.TrimSuffix(rest, "\r")
	if i := strings.LastIndexByte(rest, '\r'); i >= 0 {
		rest = rest[i+1:]
	}

	return stripEscapes(rest), stamp, stamped
}

// cutStamp splits off the RFC 3339 timestamp the forge starts a line with, and the space after it.
func cutStamp(raw string) (time.Time, string, bool) {
	if len(raw) < 21 || raw[4] != '-' || raw[10] != 'T' {
		return time.Time{}, raw, false
	}

	end := strings.IndexByte(raw[:min(len(raw), 64)], ' ')
	if end < 0 {
		return time.Time{}, raw, false
	}

	stamp, err := time.Parse(time.RFC3339Nano, raw[:end])
	if err != nil {
		return time.Time{}, raw, false
	}

	return stamp, raw[end+1:], true
}

// stripEscapes removes the terminal's control sequences (CSI) and operating system commands (OSC).
func stripEscapes(s string) string {
	if strings.IndexByte(s, '\x1b') < 0 {
		return s
	}

	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\x1b' || i+1 >= len(s) {
			b.WriteByte(s[i])

			continue
		}

		switch s[i+1] {
		case '[':
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			i = j
		case ']':
			j := i + 2
			for j < len(s) {
				if s[j] == '\a' {
					break
				}
				if s[j] == '\x1b' && j+1 < len(s) && s[j+1] == '\\' {
					j++

					break
				}
				j++
			}
			i = j
		default:
			b.WriteByte(s[i])
		}
	}

	return b.String()
}

// mapSteps finds the line each started step begins on, the first step at the first line stamped no earlier
// than a second before it started and a later one at the run group that opens it before the next step started.
func mapSteps(lines []line, steps []Step) []StepRange {
	var started []Step
	for _, step := range steps {
		if step.StartedAt != nil {
			started = append(started, step)
		}
	}

	firsts := make([]int, len(started))
	cursor := 0
	for i, step := range started {
		earliest := firstAtOrAfter(lines, cursor, step.StartedAt.Truncate(time.Second).Add(-time.Second))
		first := earliest
		if i > 0 {
			var until *time.Time
			if i+1 < len(started) {
				limit := started[i+1].StartedAt.Add(time.Second)
				until = &limit
			}
			first = findGroup(lines, earliest, until, step.Name)
		}
		firsts[i] = first
		cursor = min(first+1, len(lines))
	}

	ranges := make([]StepRange, len(started))
	for i, step := range started {
		end := len(lines)
		if i+1 < len(started) {
			end = firsts[i+1]
		}
		first := firsts[i]
		end = max(end, first)

		errorCount := 0
		for _, l := range lines[first:end] {
			if l.error {
				errorCount++
			}
		}

		ranges[i] = StepRange{
			Number:     step.Number,
			Name:       step.Name,
			Conclusion: step.Conclusion,
			FirstLine:  first + 1,
			LineCount:  end - first,
			ErrorCount: errorCount,
		}
	}

	return ranges
}

// firstAtOrAfter is the first line from the cursor stamped at or after the time, or the end of the log.
func firstAtOrAfter(lines []line, cursor int, at time.Time) int {
	for i := cursor; i < len(lines); i++ {
		if !lines[i].at.Before(at) {
			return i
		}
	}

	return len(lines)
}

// findGroup is the run group from the earliest line and stamped before until that names the step, else the
// first such group, else the earliest line.
func findGroup(lines []line, earliest int, until *time.Time, name string) int {
	first := -1
	for i := earliest; i < len(lines); i++ {
		if until != nil && !lines[i].at.Before(*until) {
			break
		}
		if lines[i].group == "" {
			continue
		}
		if lines[i].group == name {
			return i
		}
		if first < 0 {
			first = i
		}
	}

	if first >= 0 {
		return first
	}

	return earliest
}
