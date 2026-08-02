package doctor

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// The escapes a status is drawn in, matching what the log reader already uses for a level
// so the two commands look like they come from the same tool.
var statusColors = map[Status]string{
	StatusOK:   "\033[32m",
	StatusWarn: "\033[33m",
	StatusFail: "\033[31m",
	StatusSkip: "\033[90m",
}

const colorReset = "\033[0m"

// markers are what a status is drawn as. They are ASCII rather than glyphs because this
// output's second home is a pasted issue, where a terminal's idea of a symbol is not the
// reader's.
// markerWidth is how wide every marker is, which they all share so the columns after them
// do not move with the status.
const markerWidth = 2

var markers = map[Status]string{
	StatusOK:   "ok",
	StatusWarn: "--",
	StatusFail: "xx",
	StatusSkip: "..",
}

// The columns a finding is drawn in: a gutter, the marker, a gap, the name padded to the
// widest, then this before what the check found. A fix is indented past all of it so it
// sits under the reason it repairs.
const (
	gutter  = "  "
	gap     = "   "
	nameGap = "  "
)

// WriteJSON writes the report as the machine-readable document, indented because the
// reader is as often a person running --json as it is a script.
func WriteJSON(out io.Writer, report Report) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("failed to write the report: %w", err)
	}

	return nil
}

// WriteText writes the report for a person: one line per check, and an indented fix under
// anything that needs one.
func WriteText(out io.Writer, report Report, color bool) error {
	var page strings.Builder
	width := nameWidth(report.Findings)
	indent := strings.Repeat(" ", len(gutter)+markerWidth+len(gap)+width+len(nameGap))

	page.WriteString("\n")
	for _, finding := range report.Findings {
		name := fmt.Sprintf("%-*s", width, finding.Name)
		page.WriteString(gutter + marker(finding.Status, color) + gap + name + nameGap + line(finding) + "\n")

		// Only under a finding that is not ok, since a fix is an instruction and a
		// passing check has nothing to instruct.
		if finding.Status != StatusOK && finding.Fix != "" {
			page.WriteString(dim(indent+"fix: "+finding.Fix, color) + "\n")
		}
	}

	page.WriteString("\n" + summaryLine(report.Summary) + "\n")

	if _, err := io.WriteString(out, page.String()); err != nil {
		return fmt.Errorf("failed to write the report: %w", err)
	}

	return nil
}

// line is what a check reports beside its name.
//
// A failure shows only the reason, which every problem writes to stand on its own, since
// pairing it with the observation it was drawn from says the same thing twice. A warning
// shows both, because there the observation is the point and the reason is why it could
// not be judged.
func line(finding Finding) string {
	switch {
	case finding.Status == StatusFail:
		return finding.Error
	case finding.Status == StatusWarn && finding.Detail != "":
		return finding.Detail + ", " + finding.Error
	case finding.Status == StatusWarn:
		return finding.Error
	default:
		return finding.Detail
	}
}

// nameWidth is how wide the name column has to be for the details to line up down the
// page, which is what makes a list of twelve checks scannable rather than read.
func nameWidth(findings []Finding) int {
	width := 0
	for _, finding := range findings {
		if len(finding.Name) > width {
			width = len(finding.Name)
		}
	}

	return width
}

// summaryLine is the last thing on screen, and says what to do rather than only counting.
func summaryLine(summary Summary) string {
	if summary.Healthy && summary.Warn == 0 {
		return fmt.Sprintf("%d checks, all ok.", summary.Total)
	}

	counted := fmt.Sprintf("%d of %d checks failed", summary.Fail, summary.Total)
	if summary.Warn > 0 {
		counted += fmt.Sprintf(", %d worth a look", summary.Warn)
	}

	return counted + "."
}

func marker(status Status, color bool) string {
	text, ok := markers[status]
	if !ok {
		text = "??"
	}
	if !color {
		return text
	}

	return statusColors[status] + text + colorReset
}

func dim(text string, color bool) string {
	if !color {
		return text
	}

	return "\033[90m" + text + colorReset
}
