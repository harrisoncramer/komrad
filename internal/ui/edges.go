package ui

import "komrad/internal/ui/screen"

// edgeStep is how many columns one press of an edge key moves the edge.
const edgeStep = 4

// keyRegion is one of the regions side by side across the panel's window.
type keyRegion int

const (
	regionList keyRegion = iota
	// regionBlock is the block beside the list, rendered by the panel or run in the column pane.
	regionBlock
	regionStage
)

// pushEdge moves an edge of the region holding the keys a step left for a negative direction and
// right for a positive one, taking the region's other edge where it has none on that side.
func (m *model) pushEdge(direction int) {
	delta := direction * edgeStep
	floor := m.edgeFloor()

	switch m.atRegion() {
	case regionList:
		m.frame.Nudge(delta, floor)
	case regionBlock:
		if direction < 0 || !m.frame.HasStageEdge() {
			m.frame.Nudge(delta, floor)
		} else {
			m.frame.NudgeStage(delta, floor)
		}
	case regionStage:
		if m.frame.HasStageEdge() {
			m.frame.NudgeStage(delta, floor)
		} else {
			m.frame.Nudge(delta, floor)
		}
	}

	m.relayout()
}

// edgeFloor is the narrowest an edge leaves the region right of it, which is the block's floor
// wherever the panel renders one.
func (m *model) edgeFloor() int {
	if !m.frame.IsSolo() && !m.frame.HasStageEdge() {
		return 1
	}

	return m.columnOf().floor + dividerCols
}

// atRegion is the region holding the keys.
func (m *model) atRegion() keyRegion {
	switch {
	case m.term.keysElsewhere && m.frame.HasColumnKeys():
		return regionBlock
	case m.term.keysElsewhere:
		return regionStage
	case m.isColumnKeyed():
		return regionBlock
	}

	return regionList
}

// isColumnKeyed reports whether the keys are on the block the panel renders beside its list.
func (m *model) isColumnKeyed() bool {
	if !m.columnGeometry().On {
		return false
	}

	switch {
	case m.isMessagesFocused(), m.form.isOpening() && !m.isPopupForm(), m.isDocFocused():
		return true
	case m.screen == screen.Review:
		return m.review.isDiffFocused(m, m.inputs())
	}

	b, ok := m.active().(columnFocuser)

	return ok && b.isBlockFocused(m, m.inputs())
}
