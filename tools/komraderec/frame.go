package main

import (
	"strings"
)

const (
	headerOn  = "\x1b[7m"
	attrReset = "\x1b[0m"
)

type frame struct {
	lines []string
}

type stitchParams struct {
	lines []string
	label string
	cols  int
	rows  int
}

func stitch(params stitchParams) frame {
	lines := make([]string, 0, params.rows+1)
	lines = append(lines, headerOn+fitHeader("panel", params.label, params.cols)+attrReset)

	for row := range params.rows {
		line := ""
		if row < len(params.lines) {
			line = params.lines[row]
		}

		lines = append(lines, line+attrReset)
	}

	return frame{lines: lines}
}

func fitHeader(name, label string, cols int) string {
	left := []rune(" " + name)
	right := []rune("")
	if label != "" {
		right = []rune(label + " ")
	}

	if len(right) > cols {
		right = right[:cols]
	}

	room := cols - len(right)
	if len(left) > room {
		left = left[:room]
	}

	return string(left) + strings.Repeat(" ", room-len(left)) + string(right)
}

func (f frame) equals(other frame) bool {
	if len(f.lines) != len(other.lines) {
		return false
	}

	for i := range f.lines {
		if f.lines[i] != other.lines[i] {
			return false
		}
	}

	return true
}

func (f frame) file() string {
	return strings.Join(f.lines, "\n") + "\n"
}

func (f frame) screen() string {
	return "\x1b[2J\x1b[H" + strings.Join(f.lines, "\r\n")
}
