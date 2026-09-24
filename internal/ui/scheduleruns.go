package ui

import (
	"fmt"
	"strings"

	"weave/internal/ui/paint"
	"weave/internal/ui/paint/label"
	"weave/internal/ui/panel"
	schedulev1 "weave/proto/gen/schedule/v1"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// runTimeLayout is how a firing's occurrence is written at the head of its row.
const runTimeLayout = "01-02 15:04"

// runWordWidth is the column the outcome word is padded to, so every row's detail lines up.
const runWordWidth = 9

// runOutcome is how one firing is drawn: a coloured word, what goes beside it, and what goes on
// the row under it.
type runOutcome struct {
	word   string
	tone   paint.Tone
	detail string
	below  string
}

// scheduleRunRows is the rows a schedule's firings take in its document, indented to the
// document's own margin and cut to its width.
func scheduleRunRows(runs []panel.ScheduleRun, width int) []string {
	indent := strings.Repeat(" ", docMargin())

	if len(runs) == 0 {
		return []string{fitRow(indent+dimStyle.Render("never fired"), width)}
	}

	under := indent + strings.Repeat(" ", len(runTimeLayout)+2+runWordWidth+2)
	rows := make([]string, 0, len(runs))
	for _, run := range runs {
		outcome := describeRun(run)

		row := indent + dimStyle.Render(run.ScheduledFor.Format(runTimeLayout)) + "  " +
			paint.Panel.Style(outcome.tone).Render(fmt.Sprintf("%-*s", runWordWidth, outcome.word))
		if outcome.detail != "" {
			row += "  " + dimStyle.Render(outcome.detail)
		}

		rows = append(rows, fitRow(row, width))
		if outcome.below != "" {
			rows = append(rows, fitRow(under+dimStyle.Render(outcome.below), width))
		}
	}

	return rows
}

// describeRun is the word a firing's state reads as, and what is said beside it.
func describeRun(run panel.ScheduleRun) runOutcome {
	message := strings.Join(strings.Fields(run.Message), " ")
	tries := fmt.Sprintf("%d/%d", run.Attempt, run.MaxAttempts)

	switch run.State {
	case schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_COMPLETED:
		return runOutcome{word: "launched", tone: paint.ToneSuccess, detail: label.Count(run.Attempt, "attempt", "attempts")}
	case schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_CANCELLED:
		return runOutcome{word: "skipped", tone: paint.ToneWarning, detail: message}
	case schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_DISCARDED:
		return runOutcome{word: "failed", tone: paint.ToneDanger, detail: tries + " attempts", below: message}
	case schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_RETRYABLE:
		return runOutcome{word: "retrying", tone: paint.ToneWarning, detail: "attempt " + tries, below: message}
	case schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_RUNNING:
		return runOutcome{word: "running", tone: paint.ToneAccent, detail: "attempt " + tries}
	case schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_UNSPECIFIED,
		schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_AVAILABLE,
		schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_PENDING,
		schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_SCHEDULED:
	}

	return runOutcome{word: "pending", tone: paint.ToneMuted}
}

// docMargin is how far the stage's markdown indents its text, which rows drawn beside it match.
func docMargin() int {
	margin := columnStyle().Document.Margin
	if margin == nil {
		return 0
	}

	return int(*margin)
}

// fitRow cuts a styled row to a width.
func fitRow(row string, width int) string {
	if lipgloss.Width(row) <= width {
		return row
	}

	return ansi.Truncate(row, width, "…")
}
