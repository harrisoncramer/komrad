package ui

import (
	"fmt"
	"strings"

	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"

	"github.com/charmbracelet/x/ansi"
)

// changeView is what the sidebar says about the change under review, which a higher fold joins
// onto fewer rows before they wrap.
func (r reviewScreen) changeView(m *model, fold int) string {
	stat, delivery := r.diff.statView(), r.threads.deliveryCountView()

	var rows []string
	switch fold {
	case 0:
		rows = []string{r.scopeStepView(m), stat, delivery}
	case 1:
		rows = []string{r.scopeStepView(m), joinViews(stat, delivery)}
	default:
		rows = []string{joinViews(r.scopeView(), stat, delivery)}
	}
	rows = append(rows, r.sendView(m))

	width := lineWidthOf(m.paneWidth())

	var out strings.Builder
	for _, row := range rows {
		if row != "" {
			out.WriteString(ansi.Wrap(row, width, "") + "\n")
		}
	}
	if out.Len() == 0 {
		return ""
	}

	return out.String() + "\n"
}

// joinViews strings the parts that say something together on one row.
func joinViews(parts ...string) string {
	said := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			said = append(said, part)
		}
	}

	return strings.Join(said, dimStyle().Render(separated))
}

// sendView is how many comments the reader is holding and the key that sends them.
func (r reviewScreen) sendView(m *model) string {
	if !r.threads.holdsUnsent() {
		return ""
	}

	return dimStyle().Render(m.menuChordLabel(m.keys.Review.Send) + " sends " +
		label.Counted(r.threads.unsentCount(), "comment") + " to the agent")
}

// statView is how big the change is, with the counts coloured the way the lines themselves are.
func (d reviewDiff) statView() string {
	if d.View() == nil {
		return ""
	}

	stat := d.View().Stat()
	if stat.Files == 0 {
		return ""
	}

	out := dimStyle().Render(label.Counted(stat.Files, "file")+", ") +
		paint.Panel.Style(reviewAddedTone).Render(fmt.Sprintf("+%d", stat.Added)) + " " +
		paint.Panel.Style(reviewRemovedTone).Render(fmt.Sprintf("-%d", stat.Removed))

	if read, total := d.View().CountViewedFiles(); read > 0 {
		out += dimStyle().Render(fmt.Sprintf("  %d/%d viewed", read, total))
	}

	return out
}

// scopeStepView says what the diff on screen is measured between and the other thing the base key
// could show beside it.
func (r reviewScreen) scopeStepView(m *model) string {
	out := r.scopeView()
	if out != "" && r.doesFitBaseLegend(m) {
		out += dimStyle().Render(separated + r.stepSaid(m))
	}

	return out
}

// scopeView is what the diff on screen is measured between.
func (r reviewScreen) scopeView() string {
	if r.diff.View() == nil {
		return ""
	}

	from := "all changes"
	if r.diff.spec.label != "" {
		from = r.diff.spec.label
	}

	return paint.Panel.Style(reviewScopeOnTone).Bold(true).Render(from)
}

// stepSaid is the way through the chapters, naming only the direction that has somewhere to go.
func (r reviewScreen) stepSaid(m *model) string {
	back := keyLabel(m.keys.Review.Base)
	forward := keyLabel(m.keys.Review.BaseForward)

	switch {
	case r.diff.spec.origin != nil:
		return m.menuChordLabel(m.keys.Review.Jump) + " back"
	case !r.diff.spec.isChapter():
		return back + " for the latest chapter"
	case r.diff.spec.chapter == 0:
		return forward + " for a later chapter"
	default:
		return back + " back" + separated + forward + " forward"
	}
}
