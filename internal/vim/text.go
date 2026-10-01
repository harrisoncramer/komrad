package vim

import "unicode"

func lineStart(r []rune, p int) int {
	for p > 0 && r[p-1] != '\n' {
		p--
	}

	return p
}

func lineEnd(r []rune, p int) int {
	for p < len(r) && r[p] != '\n' {
		p++
	}

	return p
}

func lineOf(r []rune, p int) int {
	n := 0
	for i := 0; i < p && i < len(r); i++ {
		if r[i] == '\n' {
			n++
		}
	}

	return n
}

func lastLine(r []rune) int {
	return lineOf(r, len(r))
}

func lineStartOf(r []rune, line int) int {
	p := 0
	for n := 0; n < line && p < len(r); p++ {
		if r[p] == '\n' {
			n++
			if n == line {
				return p + 1
			}
		}
	}

	return p
}

func firstNonBlank(r []rune, p int) int {
	p = lineStart(r, p)
	end := lineEnd(r, p)
	for p < end && isBlank(r[p]) {
		p++
	}

	return p
}

// onLine is the position in line at the column col, held inside the line's characters.
func onLine(r []rune, line, col int) int {
	start := lineStartOf(r, line)
	end := lineEnd(r, start)

	return start + min(col, max(end-1-start, 0))
}

func isBlank(c rune) bool {
	return c == ' ' || c == '\t'
}

// class is a character's kind for word motions: blank, keyword, or punctuation.
func class(c rune, big bool) int {
	switch {
	case unicode.IsSpace(c):
		return 0
	case big:
		return 1
	case c == '_' || unicode.IsLetter(c) || unicode.IsDigit(c):
		return 1
	default:
		return 2
	}
}

func wordForward(r []rune, p int, big bool) int {
	n := len(r)
	if p >= n {
		return n
	}

	if c := class(r[p], big); c != 0 {
		for p < n && class(r[p], big) == c {
			p++
		}
	}

	for p < n && class(r[p], big) == 0 {
		p++
	}

	return p
}

func wordEnd(r []rune, p int, big bool) int {
	n := len(r)
	p++
	for p < n && class(r[p], big) == 0 {
		p++
	}
	if p >= n {
		return n - 1
	}

	c := class(r[p], big)
	for p+1 < n && class(r[p+1], big) == c {
		p++
	}

	return p
}

func wordBackward(r []rune, p int, big bool) int {
	p--
	for p > 0 && class(r[p], big) == 0 {
		p--
	}
	if p <= 0 {
		return 0
	}

	c := class(r[p], big)
	for p > 0 && class(r[p-1], big) == c {
		p--
	}

	return p
}

func splice(r []rune, start, end int, with []rune) []rune {
	out := make([]rune, 0, len(r)-(end-start)+len(with))
	out = append(out, r[:start]...)
	out = append(out, with...)

	return append(out, r[end:]...)
}

func toggleCase(c rune) rune {
	if unicode.IsUpper(c) {
		return unicode.ToLower(c)
	}

	return unicode.ToUpper(c)
}
