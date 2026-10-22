// Package zone marks where the panel renders a block or a row, and reads those marks back from a frame.
package zone

import (
	"encoding/hex"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"komrad/internal/panes"

	"charm.land/lipgloss/v2"
)

// Name names a block the panel renders inside its own pane.
type Name string

// Search names the search line over a list or a block.
const Search Name = "search"

// SearchList and SearchBlock key the search line by the list or the block it searches.
const (
	SearchList  = "list"
	SearchBlock = "block"
)

// Hit is the marked row under a cell.
type Hit struct {
	Zone Name
	Key  string
	Nth  int
}

// A mark is an invisible tag hidden in a frame, wrapped in escape codes the terminal skips over.
const (
	commandStart     = "\x1b_"
	stringTerminator = "\x1b\\"
	markPrefix       = "kz"
	blockStartTag    = "s"
	blockEndTag      = "e"
	rowTag           = "r"
	fieldSeparator   = ":"
)

// blockMark finds the tags at each end of a block, which read like kzs:list:40 where a list 40 cells
// wide starts and kze:list: where it ends.
var blockMark = regexp.MustCompile(regexp.QuoteMeta(commandStart+markPrefix) +
	"([" + blockStartTag + blockEndTag + "])" + fieldSeparator + "([a-z]+)" + fieldSeparator + "([0-9]*)" +
	regexp.QuoteMeta(stringTerminator))

// keyMark finds the tag in front of a row, which reads like kzr:menu:12:0:6131 for a menu row 12
// cells wide whose key is "a1" spelled in hex.
var keyMark = regexp.MustCompile(regexp.QuoteMeta(commandStart+markPrefix+rowTag) +
	fieldSeparator + "([a-z]+)" + fieldSeparator + "([0-9]+)" + fieldSeparator + "([0-9]+)" + fieldSeparator + "([0-9a-f]*)" +
	regexp.QuoteMeta(stringTerminator))

// mark is one tag with its fields, wrapped in the escape codes.
func mark(tag string, fields ...string) string {
	return commandStart + markPrefix + tag + fieldSeparator + strings.Join(fields, fieldSeparator) + stringTerminator
}

// MarkBlock wraps a block in the marks that say where it was rendered and how wide it is.
func MarkBlock(name Name, block string, width int) string {
	if block == "" {
		return block
	}

	return mark(blockStartTag, string(name), strconv.Itoa(width)) + block + mark(blockEndTag, string(name), "")
}

// MarkKey is the mark that opens a row of a zone, naming its key, how wide it is, and its nth among
// the rows sharing that key.
func MarkKey(name Name, width, nth int, key string) string {
	return mark(rowTag, string(name), strconv.Itoa(width), strconv.Itoa(nth), hex.EncodeToString([]byte(key)))
}

// StripKeys takes the row marks out of a frame.
func StripKeys(frame string) string {
	return keyMark.ReplaceAllString(frame, "")
}

// found is one mark matched on a line, with the column it sits at and the text of each group it captured.
type found struct {
	left   int
	groups []string
}

func findAll(pattern *regexp.Regexp, line string) []found {
	var marks []found
	for _, bounds := range pattern.FindAllStringSubmatchIndex(line, -1) {
		matchStart := bounds[0]

		var groups []string
		for i := 2; i < len(bounds); i += 2 {
			groupStart, groupEnd := bounds[i], bounds[i+1]
			groups = append(groups, line[groupStart:groupEnd])
		}

		marks = append(marks, found{
			left:   lipgloss.Width(line[:matchStart]),
			groups: groups,
		})
	}

	return marks
}

// Scan finds where each marked block sits in a frame, in cells from its top left.
func Scan(frame string) map[Name]panes.Rect {
	type opened struct {
		left, top, width int
	}

	starts := map[Name]opened{}
	blocks := map[Name]panes.Rect{}

	for row, line := range strings.Split(frame, "\n") {
		for _, edge := range findAll(blockMark, line) {
			tag, name, widthField := edge.groups[0], Name(edge.groups[1]), edge.groups[2]

			if tag == blockStartTag {
				width, err := strconv.Atoi(widthField)
				if err != nil {
					continue
				}

				starts[name] = opened{
					left:  edge.left,
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
			if edge.left == 0 && row > start.top {
				bottom--
			}

			blocks[name] = panes.Rect{
				Pane:   string(name),
				Left:   start.left,
				Top:    start.top,
				Width:  start.width,
				Height: bottom - start.top + 1,
			}
		}
	}

	return blocks
}

// HitAt is the innermost marked row covering column x of one line of a frame.
func HitAt(line string, x int) (Hit, bool) {
	for _, row := range slices.Backward(findAll(keyMark, line)) {
		name, widthField, nthField, keyField := Name(row.groups[0]), row.groups[1], row.groups[2], row.groups[3]

		width, err := strconv.Atoi(widthField)
		if err != nil || x < row.left || x >= row.left+width {
			continue
		}

		nth, err := strconv.Atoi(nthField)
		if err != nil {
			continue
		}

		key, err := hex.DecodeString(keyField)
		if err != nil {
			continue
		}

		return Hit{
			Zone: name,
			Key:  string(key),
			Nth:  nth,
		}, true
	}

	return Hit{}, false
}
