package ui

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"

	"komrad/internal/errui"
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/internal/view"
	"komrad/lib/xslice"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (c *statusCheckLogsScreen) view(m *model) string {
	return m.columnListView(c.listParams(m))
}

func (c *statusCheckLogsScreen) snapshot(m *model, pane listPane) view.Snapshot {
	rows := c.rows(m)

	return view.Snapshot{
		Rows: xslice.Map(rows, func(r statusCheckLogRow) view.Row {
			name := statusCheckLogRowName(r)
			row := view.Row{
				Key:     r.key(),
				Kind:    view.KindResult,
				Name:    name,
				Matches: matchesIn(m.filter.pattern, name),
				Process: view.ProcessNone,
				Cells:   []view.Cell{statusCheckIcon(r.item, m.loading.frame())},
			}
			if r.step != nil {
				row.Kind = view.KindChild
				row.Cells = []view.Cell{statusCheckLogStepIcon(r.step)}
			}

			return row
		}),
		Caret:   pane.caret,
		Focused: rowKeyAt(rows, c.cursor),
		Pane:    pane.width,
		Columns: statusCheckColumns(),
	}
}

// statusCheckLogStepIcon is the icon of a step of a log, blank for a step that reported no conclusion.
func statusCheckLogStepIcon(step *pullrequestv1.CheckLogStep) view.Cell {
	if step.GetConclusion() == "" {
		return view.Cell{}
	}

	return statusCheckIcon(statusCheckItem{
		state:      pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED,
		conclusion: step.GetConclusion(),
		passed:     step.GetPassed(),
	}, "")
}

// doc is the logs of the shown checks rendered at a width, with the row each list row's header is rendered on.
func (c *statusCheckLogsScreen) doc(m *model, width int) statusCheckLogsRendered {
	shown := c.shown(m)
	shape := c.docShape(shown, width)
	if c.rendered.shape == shape {
		return c.rendered
	}

	indent := strings.Repeat(" ", docMargin())
	wrap := max(width-2*docMargin(), 1)
	var rows []string
	anchors := map[string]int{}
	line := func(text string, style lipgloss.Style) {
		rows = append(rows, xslice.Map(strings.Split(ansi.Wrap(text, wrap, ""), "\n"), func(part string) string {
			return fitRow(indent+style.Render(part), width)
		})...)
	}

	for i, item := range shown {
		if i > 0 {
			rows = append(rows, "")
		}

		anchors[statusCheckLogRow{item: item}.key()] = len(rows)
		title := "═ " + item.name + " "
		rows = append(rows, fitRow(indent+lipgloss.NewStyle().Bold(true).Render(title+strings.Repeat("═", max(wrap-lipgloss.Width(title), 0))), width))

		if notice := c.logNotice(item); notice != "" {
			line(notice, dimStyle())

			continue
		}

		steps := slices.SortedStableFunc(slices.Values(item.log.GetSteps()), func(a, b *pullrequestv1.CheckLogStep) int {
			return cmp.Compare(a.GetFirstLine(), b.GetFirstLine())
		})
		header := func(upTo int) {
			for len(steps) > 0 && int(steps[0].GetFirstLine()) <= upTo {
				anchors[statusCheckLogRow{item: item, step: steps[0]}.key()] = len(rows)
				line(glyph.Step+" "+steps[0].GetName(), lipgloss.NewStyle().Bold(true))
				steps = steps[1:]
			}
		}

		read := c.logs[item.checkID]
		for n, text := range read.lines {
			header(n + 1)

			style := lipgloss.NewStyle()
			if strings.HasPrefix(text, statusCheckLogErrorMarker) {
				style = paint.Panel.Style(paint.ToneDanger)
			}
			line(strings.ReplaceAll(text, "\t", "    "), style)
		}
		if !read.reading {
			header(math.MaxInt)
		}
	}

	return statusCheckLogsRendered{
		shape:   shape,
		rows:    rows,
		anchors: anchors,
	}
}

// docShape names everything the document is rendered from, so it is rendered again only once that changes.
func (c *statusCheckLogsScreen) docShape(shown []statusCheckItem, width int) string {
	var shape strings.Builder
	fmt.Fprintf(&shape, "%d/%d", c.generation, width)
	for _, item := range shown {
		read := c.logs[item.checkID]
		lines, reading, failed := 0, false, false
		if read != nil {
			lines, reading, failed = len(read.lines), read.reading, read.err != nil
		}
		fmt.Fprintf(&shape, "/%s:%d:%d:%t:%t", item.checkID, item.log.GetState(), lines, reading, failed)
	}

	return shape.String()
}

// logNotice is what is rendered under a check in place of its log, empty while there are lines to render.
func (c *statusCheckLogsScreen) logNotice(item statusCheckItem) string {
	read := c.logs[item.checkID]
	switch {
	case read != nil && read.err != nil:
		return "could not read: " + errui.GetReason(read.err)
	case read != nil && len(read.lines) > 0:
		return ""
	case read != nil && read.reading:
		return "reading…"
	}

	switch item.log.GetState() {
	case pullrequestv1.CheckLogState_CHECK_LOG_STATE_FETCHED:
		return "empty log"
	case pullrequestv1.CheckLogState_CHECK_LOG_STATE_PENDING:
		return "log not fetched yet"
	case pullrequestv1.CheckLogState_CHECK_LOG_STATE_TOO_LARGE:
		return "log too large to keep"
	case pullrequestv1.CheckLogState_CHECK_LOG_STATE_MISSING:
		return "the forge has no log for this check"
	case pullrequestv1.CheckLogState_CHECK_LOG_STATE_EXPIRED:
		return "log expired on the forge"
	case pullrequestv1.CheckLogState_CHECK_LOG_STATE_FAILED:
		return "log could not be fetched"
	case pullrequestv1.CheckLogState_CHECK_LOG_STATE_UNSPECIFIED, pullrequestv1.CheckLogState_CHECK_LOG_STATE_NONE:
	}

	return "no log"
}
