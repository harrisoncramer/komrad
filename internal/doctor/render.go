package doctor

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// The columns a finding is drawn in. A gutter, the marker, a gap, the name padded to the
// widest, then nameGap before what the check found. Every marker is markerWidth wide so the
// columns after it do not move with the status.
const (
	gutter      = "  "
	gap         = "   "
	nameGap     = "  "
	markerWidth = 2

	colorReset = "\033[0m"
	dimColor   = "\033[90m"
)

// The escapes a status is drawn in, matching what the log reader uses for a level so the
// two commands look like one tool.
var statusColors = map[Status]string{
	StatusOK:   "\033[32m",
	StatusWarn: "\033[33m",
	StatusFail: "\033[31m",
	StatusSkip: dimColor,
}

// markers are what a status is drawn as: ASCII rather than glyphs, because this output's
// second home is a pasted issue.
var markers = map[Status]string{
	StatusOK:   "ok",
	StatusWarn: "--",
	StatusFail: "xx",
	StatusSkip: "..",
}

// WriteJSON writes the report as the machine-readable document, indented because the reader
// is as often a person running --json as a script.
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
	width := measureNameColumn(report.Findings)
	indent := strings.Repeat(" ", len(gutter)+markerWidth+len(gap)+width+len(nameGap))

	page.WriteString("\n")
	for _, finding := range report.Findings {
		name := fmt.Sprintf("%-*s", width, finding.Name)
		page.WriteString(gutter + renderMarker(finding.Status, color) + gap + name + nameGap + describeFinding(finding) + "\n")

		// A fix is an instruction, and a passing check has nothing to instruct.
		if finding.Status != StatusOK && finding.Fix != "" {
			page.WriteString(renderDim(indent+"fix: "+finding.Fix, color) + "\n")
		}
	}

	page.WriteString("\n" + describeSummary(report.Summary) + "\n")

	if _, err := io.WriteString(out, page.String()); err != nil {
		return fmt.Errorf("failed to write the report: %w", err)
	}

	return nil
}

// describeFinding is what a check reports beside its name; anything not ok shows the reason
// alone, since every problem stands on its own and pairing it with the observation it was
// drawn from would say the same thing twice. The observation stays in the JSON.
func describeFinding(finding Finding) string {
	if finding.Error != "" {
		return foldToOneLine(finding.Error)
	}

	return foldToOneLine(finding.Detail)
}

// foldToOneLine folds a reason onto the row it has to fit on; a parser reports what it
// objected to over several lines, and one landing mid-table takes the columns with it.
func foldToOneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// measureNameColumn is how wide the name column has to be for the details to line up, which
// is what makes a dozen checks scannable rather than read.
func measureNameColumn(findings []Finding) int {
	width := 0
	for _, finding := range findings {
		if len(finding.Name) > width {
			width = len(finding.Name)
		}
	}

	return width
}

// describeSummary is the last thing on screen, and says what to do rather than only counting.
func describeSummary(summary Summary) string {
	if summary.Healthy && summary.Warn == 0 {
		return fmt.Sprintf("%d checks, all ok.", summary.Total)
	}

	counted := fmt.Sprintf("%d of %d checks failed", summary.Fail, summary.Total)
	if summary.Warn > 0 {
		counted += fmt.Sprintf(", %d worth a look", summary.Warn)
	}

	return counted + "."
}

// renderMarker draws a status in its own colour, padded by markers to a common width.
func renderMarker(status Status, color bool) string {
	text, known := markers[status]
	if !known {
		text = "??"
	}
	if !color {
		return text
	}

	return statusColors[status] + text + colorReset
}

// renderDim draws secondary text, which is every fix line.
func renderDim(text string, color bool) string {
	if !color {
		return text
	}

	return dimColor + text + colorReset
}

// joinWords lists names the way a sentence would, so a message offering choices reads as one
// rather than a slice printed out.
func joinWords(names []string) string {
	switch len(names) {
	case 0:
		return "None"
	case 1:
		return names[0]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
	}
}
