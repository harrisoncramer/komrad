package ui

import (
	"encoding/hex"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"komrad/internal/panes"
	"komrad/internal/view"

	"charm.land/lipgloss/v2"
)

// zone names a block the panel renders inside its own pane.
type zone string

const (
	zoneList        zone = "list"
	zoneColumn      zone = "column"
	zoneDetails     zone = "details"
	zoneBlock       zone = "block"
	zonePullRequest zone = "pullrequest"
	zoneViews       zone = "views"
	zoneKinds       zone = "kinds"
	zoneTabs        zone = "tabs"
	zoneFiles       zone = "files"
	zoneSearch      zone = "search"
	zoneMessages    zone = "messages"
	zoneDoc         zone = "doc"
	zoneChange      zone = "change"
)

// zoneMark is the invisible sequence a marked frame carries at each end of a zone.
var zoneMark = regexp.MustCompile("\x1b_kz([se]):([a-z]+):([0-9]*)\x1b\\\\")

// markZone wraps a block in the marks that say where it was rendered, while the panel is rendering a
// marked frame.
func (m *model) markZone(name zone, block string, width int) string {
	if !m.term.markingZones || block == "" {
		return block
	}

	return "\x1b_kzs:" + string(name) + ":" + strconv.Itoa(width) + "\x1b\\" + block + "\x1b_kze:" + string(name) + ":\x1b\\"
}

// isMarkingZones reports whether this frame is rendered to find where its blocks landed.
func (m *model) isMarkingZones() bool {
	return m.term.markingZones
}

// scanZones finds where each marked zone sits in a frame, in cells from its top left.
func scanZones(frame string) map[zone]panes.Rect {
	type opened struct {
		left, top, width int
	}

	starts := map[zone]opened{}
	found := map[zone]panes.Rect{}

	for row, line := range strings.Split(frame, "\n") {
		for _, at := range zoneMark.FindAllStringSubmatchIndex(line, -1) {
			name := zone(line[at[4]:at[5]])
			col := lipgloss.Width(line[:at[0]])

			if line[at[2]:at[3]] == "s" {
				width, err := strconv.Atoi(line[at[6]:at[7]])
				if err != nil {
					continue
				}

				starts[name] = opened{
					left:  col,
					top:   row,
					width: width,
				}

				continue
			}

			start, ok := starts[name]
			if !ok {
				continue
			}

			bottom := row
			if col == 0 && row > start.top {
				bottom--
			}

			found[name] = panes.Rect{
				Pane:   string(name),
				Left:   start.left,
				Top:    start.top,
				Width:  start.width,
				Height: bottom - start.top + 1,
			}
		}
	}

	return found
}

// sidebarBlock is a block the panel rendered inside its own pane that the picker can land on.
type sidebarBlock struct {
	stop stop
	rect panes.Rect
}

// findSidebarBlocks renders the panel once with its blocks marked and reads where each one landed.
func (m *model) findSidebarBlocks() []sidebarBlock {
	marked := *m
	marked.term.markingZones = true

	zones := scanZones(marked.renderInset(marked.insideView()))
	stops := m.listStops()
	height := m.term.height

	var blocks []sidebarBlock

	details, hasDetails := zones[zoneDetails]
	if hasDetails && slices.Contains(stops, stopRecord) {
		blocks = append(blocks, sidebarBlock{stop: stopRecord, rect: details})
		height = details.Top
	}

	list := panes.Rect{
		Left:   0,
		Top:    0,
		Width:  m.term.width,
		Height: height,
	}
	column, hasColumn := zones[zoneColumn]
	if hasColumn {
		if !m.frame.IsSidebarOnRight() {
			list.Width = column.Left - dividerCols
		} else {
			list.Left = column.Left + column.Width + dividerCols
			list.Width = m.term.width - list.Left
		}
	}

	if marked, ok := zones[zoneList]; ok {
		list = marked
	}

	block, hasBlock := zones[zoneBlock]
	if !hasBlock {
		block, hasBlock = column, hasColumn
	}

	if hasBlock && slices.Contains(stops, stopBlock) {
		blocks = append(blocks, sidebarBlock{stop: stopBlock, rect: block})
	}

	if pullRequest, ok := zones[zonePullRequest]; ok && slices.Contains(stops, stopPullRequest) {
		blocks = append(blocks, sidebarBlock{stop: stopPullRequest, rect: pullRequest})
	}

	return append([]sidebarBlock{{stop: stopList, rect: list}}, blocks...)
}

// clipRect is the part of rect inside bounds, and whether any of it is.
func clipRect(rect, bounds panes.Rect) (panes.Rect, bool) {
	left, top := max(rect.Left, bounds.Left), max(rect.Top, bounds.Top)
	right := min(rect.Left+rect.Width, bounds.Left+bounds.Width)
	bottom := min(rect.Top+rect.Height, bounds.Top+bounds.Height)

	if right <= left || bottom <= top {
		return panes.Rect{}, false
	}

	return panes.Rect{
		Pane:   rect.Pane,
		Left:   left,
		Top:    top,
		Width:  right - left,
		Height: bottom - top,
	}, true
}

// isInRect is whether the cell at x, y falls inside rect.
func isInRect(rect panes.Rect, x, y int) bool {
	return x >= rect.Left && x < rect.Left+rect.Width && y >= rect.Top && y < rect.Top+rect.Height
}

var keyMark = regexp.MustCompile("\x1b_kzr:([a-z]+):([0-9]+):([0-9]+):([0-9a-f]*)\x1b\\\\")

func (m *model) rowMarker(name zone, rows []view.Row, width int) func(int, string) string {
	if !m.term.markingZones {
		return nil
	}

	return func(index int, row string) string {
		key := rows[index].Key
		if key == "" {
			return row
		}

		nth := 0
		for _, earlier := range rows[:index] {
			if earlier.Key == key {
				nth++
			}
		}

		return keyMarkOf(name, width, nth, key) + row
	}
}

func (m *model) spanMarker(name zone, keys []string) func(int, string) string {
	if !m.term.markingZones {
		return nil
	}

	return func(index int, span string) string {
		if keys[index] == "" {
			return span
		}

		return m.markSpan(name, keys[index], span)
	}
}

func (m *model) markSpan(name zone, key, span string) string {
	if !m.term.markingZones {
		return span
	}

	return keyMarkOf(name, lipgloss.Width(span), 0, key) + span
}

func (m *model) markSearch(to stop, width int, line string) string {
	return m.markRow(zoneSearch, width, strconv.Itoa(int(to)), line)
}

func (m *model) markRow(name zone, width int, key, row string) string {
	if !m.term.markingZones {
		return row
	}

	return keyMarkOf(name, width, 0, key) + row
}

func keyMarkOf(name zone, width, nth int, key string) string {
	return "\x1b_kzr:" + string(name) + ":" + strconv.Itoa(width) + ":" + strconv.Itoa(nth) + ":" + hex.EncodeToString([]byte(key)) + "\x1b\\"
}

type keyHit struct {
	zone zone
	key  string
	nth  int
}

func (m *model) findKeyAt(x, y int) (keyHit, bool) {
	marked := *m
	marked.term.markingZones = true

	lines := strings.Split(marked.renderInset(marked.insideView()), "\n")
	if y < 0 || y >= len(lines) {
		return keyHit{}, false
	}

	line := lines[y]
	marks := keyMark.FindAllStringSubmatchIndex(line, -1)
	for _, at := range slices.Backward(marks) {
		left := lipgloss.Width(line[:at[0]])
		width, err := strconv.Atoi(line[at[4]:at[5]])
		if err != nil || x < left || x >= left+width {
			continue
		}

		nth, err := strconv.Atoi(line[at[6]:at[7]])
		if err != nil {
			continue
		}

		key, err := hex.DecodeString(line[at[8]:at[9]])
		if err != nil {
			continue
		}

		return keyHit{zone: zone(line[at[2]:at[3]]), key: string(key), nth: nth}, true
	}

	return keyHit{}, false
}
