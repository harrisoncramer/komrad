package vim

import "math"

// reach is how far a motion's range runs past its target.
type reach int

const (
	exclusive reach = iota
	inclusive
	linewise
)

// find is an f, F, t or T search.
type find struct {
	name string
	char rune
}

var reversed = map[string]string{"f": "F", "F": "f", "t": "T", "T": "t"}

// motion is where c moves the cursor from p, and how an operator's range reaches it.
func (e *Engine) motion(r []rune, p int, c command) (int, reach, bool) {
	count := c.times()
	start, end := lineStart(r, p), lineEnd(r, p)

	switch c.name {
	case "h":
		return max(start, p-count), exclusive, true
	case "l":
		limit := end
		if c.op == "" {
			limit = max(end-1, start)
		}

		return min(p+count, limit), exclusive, true
	case "j", "k", "enter":
		line, last := lineOf(r, p), lastLine(r)
		target := min(line+count, last)
		if c.name == "k" {
			target = max(line-count, 0)
		}
		if target == line {
			return p, linewise, false
		}
		if c.name == "enter" {
			return firstNonBlank(r, lineStartOf(r, target)), linewise, true
		}

		if !e.held {
			e.column, e.held = p-start, true
		}

		return onLine(r, target, e.column), linewise, true
	case "w", "W":
		return e.wordMotion(r, p, c)
	case "e", "E":
		for range count {
			p = wordEnd(r, p, c.name == "E")
		}

		return max(p, 0), inclusive, true
	case "b", "B":
		for range count {
			p = wordBackward(r, p, c.name == "B")
		}

		return p, exclusive, true
	case "0":
		return start, exclusive, true
	case "^":
		return firstNonBlank(r, p), exclusive, true
	case "$":
		line := min(lineOf(r, p)+count-1, lastLine(r))
		e.column, e.held = math.MaxInt, true
		end = lineEnd(r, lineStartOf(r, line))
		if c.op != "" {
			return end, exclusive, true
		}

		return max(end-1, lineStartOf(r, line)), inclusive, true
	case "G", "gg":
		line := lastLine(r)
		if c.name == "gg" {
			line = 0
		}
		if given := c.given(); given > 0 {
			line = min(given-1, lastLine(r))
		}

		return firstNonBlank(r, lineStartOf(r, line)), linewise, true
	case "f", "F", "t", "T":
		e.lastFind = &find{name: c.name, char: c.char}

		return findChar(r, p, find{name: c.name, char: c.char}, count)
	case ";", ",":
		if e.lastFind == nil {
			return p, exclusive, false
		}

		name := e.lastFind.name
		if c.name == "," {
			name = reversed[name]
		}

		from := p
		switch {
		case name == "t" && p+1 < lineEnd(r, p):
			from++
		case name == "T" && p > lineStart(r, p):
			from--
		}

		at, how, ok := findChar(r, from, find{name: name, char: e.lastFind.char}, count)
		if !ok {
			return p, how, false
		}

		return at, how, true
	}

	return p, exclusive, false
}

// wordMotion is where w or W moves the cursor from p.
func (e *Engine) wordMotion(r []rune, p int, c command) (int, reach, bool) {
	big := c.name == "W"
	if c.op == "c" && p < len(r) && class(r[p], big) != 0 {
		p--
		for range c.times() {
			p = wordEnd(r, p, big)
		}

		return p, inclusive, true
	}

	from := p
	for range c.times() {
		from = p
		p = wordForward(r, p, big)
	}

	if c.op != "" {
		if end := lineEnd(r, from); p > end {
			p = end
		}
	}

	return p, exclusive, true
}

func findChar(r []rune, p int, f find, count int) (int, reach, bool) {
	start, end := lineStart(r, p), lineEnd(r, p)

	switch f.name {
	case "f", "t":
		at := p
		for range count {
			at++
			for at < end && r[at] != f.char {
				at++
			}
			if at >= end {
				return p, inclusive, false
			}
		}
		if f.name == "t" {
			at--
		}

		return at, inclusive, true
	default:
		at := p
		for range count {
			at--
			for at >= start && r[at] != f.char {
				at--
			}
			if at < start {
				return p, exclusive, false
			}
		}
		if f.name == "T" {
			at++
		}

		return at, exclusive, true
	}
}
