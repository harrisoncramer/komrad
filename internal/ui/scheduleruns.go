package ui

import (
	"fmt"
	"strings"

	"weave/internal/errui"
	"weave/internal/ui/paint"
	"weave/internal/ui/paint/label"
	"weave/internal/ui/panel"
	schedulev1 "weave/proto/gen/schedule/v1"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// scheduleRunsShown is how many of a schedule's newest firings its document lists.
const scheduleRunsShown = 20

// runTimeLayout is how a firing's occurrence is written at the head of its row.
const runTimeLayout = "01-02 15:04"

// runWordWidth is the column the outcome word is padded to, so every row's detail lines up.
const runWordWidth = 9

// scheduleRunsRead is what the service answered for one schedule's firings.
type scheduleRunsRead struct {
	scheduleID string
	// latest is the schedule's newest firing as the listing held it when these were asked for, so
	// a listing that moves it asks again.
	latest   string
	pending  bool
	answered bool
	runs     []panel.ScheduleRun
	err      error
}

// scheduleRunsReadMsg is one schedule's firings come back.
type scheduleRunsReadMsg struct {
	read scheduleRunsRead
}

// runOutcome is how one firing is drawn: a coloured word, what goes beside it, and what goes on
// the row under it.
type runOutcome struct {
	word   string
	tone   paint.Tone
	detail string
	below  string
}

// readScheduleRunsCmd asks for the hovered schedule's firings unless what stands is already for
// that schedule and its newest firing.
func (m *model) readScheduleRunsCmd() tea.Cmd {
	if m.screen != screenSchedules {
		return nil
	}

	item, ok := m.schedules.selected(m.shared())
	if !ok {
		return nil
	}

	id, latest := item.schedule.ID, runKey(item.schedule.LatestRun)
	last := m.scheduleRuns
	if last.scheduleID == id && last.latest == latest && (last.pending || last.answered) {
		return nil
	}

	next := scheduleRunsRead{scheduleID: id, latest: latest, pending: true}
	if last.scheduleID == id {
		next.answered, next.runs, next.err = last.answered, last.runs, last.err
	}
	m.scheduleRuns = next
	clients := m.api

	return func() tea.Msg {
		runs, err := clients.listScheduleRuns(id)

		return scheduleRunsReadMsg{read: scheduleRunsRead{
			scheduleID: id,
			latest:     latest,
			answered:   true,
			runs:       runs,
			err:        err,
		}}
	}
}

// applyScheduleRunsRead keeps an answer for what was last asked, and drops one the cursor or a
// newer firing has already moved past.
func (m *model) applyScheduleRunsRead(msg scheduleRunsReadMsg) {
	if msg.read.scheduleID != m.scheduleRuns.scheduleID || msg.read.latest != m.scheduleRuns.latest {
		return
	}

	m.scheduleRuns = msg.read
}

// runKey names a firing as far as asking again goes, changing whenever River moves the job.
func runKey(run *panel.ScheduleRun) string {
	if run == nil {
		return ""
	}

	return fmt.Sprintf("%d:%s:%d", run.ID, run.State, run.Attempt)
}

// scheduleRunRows is the rows a schedule's firings take in its document, indented to the
// document's own margin and cut to its width.
func scheduleRunRows(read scheduleRunsRead, width int) []string {
	indent := strings.Repeat(" ", docMargin())

	switch {
	case read.err != nil:
		return []string{fitRow(indent+paint.Panel.Style(paint.ToneDanger).Render("could not read runs: "+errui.GetReason(read.err)), width)}
	case !read.answered:
		return nil
	case len(read.runs) == 0:
		return []string{fitRow(indent+dimStyle.Render("never fired"), width)}
	}

	under := indent + strings.Repeat(" ", len(runTimeLayout)+2+runWordWidth+2)
	rows := make([]string, 0, len(read.runs))
	for _, run := range read.runs {
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

// describeRun is the word River's state for a firing reads as, and what is said beside it.
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
