package ui

import (
	"fmt"
	"strings"

	"weave/internal/review"
	"weave/internal/ui/layout"
)

var reviewStagePath = stageFilePath("review")

// reviewFrames is the size of the pane the diff is drawn into, kept here because the frame
// reports it once when it starts the drawer rather than on every repaint.
type reviewFrames struct {
	width  int
	height int
}

// window is the screenful starting at top, with the cursor's own row drawn as the cursor, dim
// while the keys are on the record under the sidebar rather than on the diff.
func (f *reviewFrames) window(view *review.View, top, cursor, pan int, focused bool) string {
	height := max(f.height, 1)

	start := max(min(top, view.Len()-height), 0)

	held := review.LiveCursor
	if !focused {
		held = review.DimCursor
	}

	// Only the rows the pane can show are drawn, since a nine thousand row diff takes a tenth of
	// a second to draw whole and panning would pay that on every keypress.
	rows := make([]string, 0, height)
	for y := start; y < min(start+height, view.Len()); y++ {
		mark := review.NoCursor
		if y == cursor {
			mark = held
		}

		rows = append(rows, view.RenderRow(y, mark, f.width, pan))
	}

	// Padded out rather than left short, so a diff smaller than the pane is followed by blank
	// lines instead of the drawer being handed fewer rows than it has.
	for len(rows) < height {
		rows = append(rows, "")
	}

	return strings.Join(rows, "\n") + "\n"
}

func (m *model) stageReview() {
	if m.review.view == nil {
		return
	}

	// Showing is what measures the pane, so the window is only settled afterwards.
	m.frame.Show(layout.RegionStage, m.reviewContent())
	m.scrollReviewToCursor()

	if err := m.repaintReview(); err != nil {
		m.report("Could not draw the diff", err)
	}
}

// repaintReviewFocus redraws the diff after the record has taken the keys or given them back,
// since the cursor row is painted dim while they are away.
func (m *model) repaintReviewFocus() {
	if err := m.repaintReview(); err != nil {
		m.report("Could not draw the diff", err)
	}
}

// repaintReview rewrites the screenful the stage is showing and tells the pager to re-read it.
func (m model) repaintReview() error {
	pane := m.frame.Pane(layout.RegionStage)

	// Nothing to draw against until the frame has started the drawer and reported its pane size.
	if pane == "" || m.review.view == nil || m.review.frames.width == 0 {
		return nil
	}

	if !m.writeReviewStage() {
		return fmt.Errorf("failed to write the diff to %s", reviewStagePath)
	}

	// The pager is left running and re-reads on any key, since restarting it repainted the whole
	// stage as a dozen partial writes over twenty milliseconds and read as a flicker.
	return m.tmux.SendKeys(pane, "R")
}

// writeReviewStage puts the screenful the pane is showing where the drawer reads it.
func (m model) writeReviewStage() bool {
	return writeStageFile(reviewStagePath, m.review.frames.window(
		m.review.view, m.review.top, m.review.cursor, m.review.pan, !m.isDetailsFocused()))
}

// reviewContent starts the pager on the diff, keyed by the agent alone so that moving the
// cursor repaints rather than respawning the pane.
func (m model) reviewContent() layout.Content {
	frames := m.review.frames

	return layout.Content{
		Key:   "review:" + m.review.agentID,
		Dir:   homeDir(),
		Wraps: true,
		Render: func(target layout.Target) ([]string, bool) {
			frames.width = max(target.Width, review.GutterWidth+20)
			frames.height = max(target.Height, 1)

			if !m.writeReviewStage() {
				return nil, false
			}

			return m.reviewStageArgv(reviewStagePath), true
		},
	}
}

// reviewStageArgv runs weave's own drawer in the stage rather than a pager, since bubbletea's
// renderer writes only the rows that differ where a pager repaints its whole screen.
func (m model) reviewStageArgv(path string) []string {
	self := m.self
	if self == "" {
		self = "weave"
	}

	return []string{self, "stage", path}
}
