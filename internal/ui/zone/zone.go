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

// Hit is the marked row under a cell.
type Hit struct {
	Zone Name
	Key  string
	Nth  int
}

const (
	commandStart     = "\x1b_"
	stringTerminator = "\x1b\\"
	markPrefix       = "kz"
	blockStartTag    = "s"
	blockEndTag      = "e"
	rowTag           = "r"
	fieldSeparator   = ":"
)

var blockMark = regexp.MustCompile(regexp.QuoteMeta(commandStart+markPrefix) +
	"([" + blockStartTag + blockEndTag + "])" + fieldSeparator + "([a-z]+)" + fieldSeparator + "([0-9]*)" +
	regexp.QuoteMeta(stringTerminator))

var keyMark = regexp.MustCompile(regexp.QuoteMeta(commandStart+markPrefix+rowTag) +
	fieldSeparator + "([a-z]+)" + fieldSeparator + "([0-9]+)" + fieldSeparator + "([0-9]+)" + fieldSeparator + "([0-9a-f]*)" +
	regexp.QuoteMeta(stringTerminator))

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

// Scan finds where each marked block sits in a frame, in cells from its top left.
func Scan(frame string) map[Name]panes.Rect {
	type opened struct {
		left, top, width int
	}

	starts := map[Name]opened{}
	found := map[Name]panes.Rect{}

	for row, line := range strings.Split(frame, "\n") {
		for _, at := range blockMark.FindAllStringSubmatchIndex(line, -1) {
			name := Name(line[at[4]:at[5]])
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

// HitAt is the innermost marked row covering column x of one line of a frame.
func HitAt(line string, x int) (Hit, bool) {
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

		return Hit{
			Zone: Name(line[at[2]:at[3]]),
			Key:  string(key),
			Nth:  nth,
		}, true
	}

	return Hit{}, false
}
