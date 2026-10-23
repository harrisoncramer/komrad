package checks

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"

	"komrad/internal/errui"
	"komrad/internal/glyph"
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/screen"
	"komrad/internal/view"
	"komrad/lib/xslice"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (c Logs) snapshot(in screen.Inputs, pane list.Pane) view.Snapshot {
	rows := c.rows(in)

	return view.Snapshot{
		Rows: xslice.Map(rows, func(r logRow) view.Row {
			name := logRowName(r)
			row := view.Row{
				Key:     r.Key(),
				Kind:    view.KindResult,
				Name:    name,
				Matches: list.Matches(in.Room.Pattern.Regexp, name),
				Process: view.ProcessNone,
				Cells:   []view.Cell{checkIcon(r.item, in.Room.SpinnerFrame)},
			}
			if r.step != nil {
				row.Kind = view.KindChild
				row.Cells = []view.Cell{stepIcon(r.step)}
			}

			return row
		}),
		Caret:   pane.Caret,
		Focused: list.KeyAt(rows, c.Row),
		Pane:    pane.Width,
		Columns: columns(),
	}
}

// stepIcon is the icon of a step of a log, blank for a step that reported no conclusion.
func stepIcon(step *pullrequestv1.CheckLogStep) view.Cell {
	if step.GetConclusion() == "" {
		return view.Cell{}
	}

	return checkIcon(check{
		state:      pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED,
		conclusion: step.GetConclusion(),
		passed:     step.GetPassed(),
	}, "")
}

// doc is the logs of the shown checks rendered at a width, with the row each list row's header is rendered on.
func (c Logs) doc(width int) logsRendered {
	shown := c.shown()
	shape := c.docShape(shown, width)
	if c.rendered.shape == shape {
		return c.rendered
	}

	wrap := max(width, 1)
	var rows []string
	anchors := map[string]int{}
	line := func(text string, style lipgloss.Style) {
		rows = append(rows, xslice.Map(strings.Split(ansi.Wrap(text, wrap, ""), "\n"), func(part string) string {
			return paint.FitRow(style.Render(part), width)
		})...)
	}

	for i, item := range shown {
		if i > 0 {
			rows = append(rows, "")
		}

		anchors[logRow{item: item}.Key()] = len(rows)
		title := "═ " + item.name + " "
		rows = append(rows, paint.FitRow(lipgloss.NewStyle().Bold(true).Render(title+strings.Repeat("═", max(wrap-lipgloss.Width(title), 0))), width))

		if notice := c.logNotice(item); notice != "" {
			line(notice, paint.Panel.Style(paint.ToneMuted))

			continue
		}

		steps := slices.SortedStableFunc(slices.Values(item.log.GetSteps()), func(a, b *pullrequestv1.CheckLogStep) int {
			return cmp.Compare(a.GetFirstLine(), b.GetFirstLine())
		})
		header := func(upTo int) {
			for len(steps) > 0 && int(steps[0].GetFirstLine()) <= upTo {
				anchors[logRow{item: item, step: steps[0]}.Key()] = len(rows)
				line(glyph.Step+" "+steps[0].GetName(), lipgloss.NewStyle().Bold(true))
				steps = steps[1:]
			}
		}

		read := c.reads[item.checkID]
		for n, text := range read.lines {
			header(n + 1)

			style := lipgloss.NewStyle()
			if strings.HasPrefix(text, logErrorMarker) {
				style = paint.Panel.Style(paint.ToneDanger)
			}
			line(strings.ReplaceAll(text, "\t", "    "), style)
		}
		if !read.reading {
			header(math.MaxInt)
		}
	}

	return logsRendered{
		shape:   shape,
		rows:    rows,
		anchors: anchors,
	}
}

// docShape names everything the document is rendered from, so it is rendered again only once that changes.
func (c Logs) docShape(shown []check, width int) string {
	var shape strings.Builder
	fmt.Fprintf(&shape, "%d/%d", c.generation, width)
	for _, item := range shown {
		read := c.reads[item.checkID]
		lines, reading, failed := 0, false, false
		if read != nil {
			lines, reading, failed = len(read.lines), read.reading, read.err != nil
		}
		fmt.Fprintf(&shape, "/%s:%d:%d:%t:%t", item.checkID, item.log.GetState(), lines, reading, failed)
	}

	return shape.String()
}

// logNotice is what is rendered under a check in place of its log, empty while there are lines to render.
func (c Logs) logNotice(item check) string {
	read := c.reads[item.checkID]
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
