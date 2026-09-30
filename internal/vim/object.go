package vim

var pairs = map[string][2]rune{"(": {'(', ')'}, "[": {'[', ']'}, "{": {'{', '}'}}

// object is the range [start, end) a text object covers around p.
func object(r []rune, p int, name string) (int, int, bool) {
	around, kind := name[0] == 'a', name[1:]

	switch kind {
	case "w", "W":
		return wordObject(r, p, around, kind == "W")
	case `"`, "'", "`":
		return quoteObject(r, p, around, []rune(kind)[0])
	}

	pair, ok := pairs[kind]
	if !ok {
		return 0, 0, false
	}

	return pairObject(r, p, around, pair)
}

// kind is a character's kind for iw and aw, where a run of blanks is a word of its own.
func kind(c rune, big bool) int {
	if isBlank(c) {
		return 0
	}

	return class(c, big)
}

func wordObject(r []rune, p int, around, big bool) (int, int, bool) {
	start, end := lineStart(r, p), lineEnd(r, p)
	if p >= end {
		return 0, 0, false
	}

	grow := func(at int) (int, int) {
		k := kind(r[at], big)
		from, to := at, at+1
		for from > start && kind(r[from-1], big) == k {
			from--
		}
		for to < end && kind(r[to], big) == k {
			to++
		}

		return from, to
	}

	from, to := grow(p)
	if !around {
		return from, to, true
	}

	if isBlank(r[p]) {
		if to < end {
			_, to = grow(to)
		}

		return from, to, true
	}

	if to < end && isBlank(r[to]) {
		_, to = grow(to)

		return from, to, true
	}

	for from > start && isBlank(r[from-1]) {
		from--
	}

	return from, to, true
}

func quoteObject(r []rune, p int, around bool, quote rune) (int, int, bool) {
	start, end := lineStart(r, p), lineEnd(r, p)

	var marks []int
	for i := start; i < end; i++ {
		if r[i] == quote && (i == start || r[i-1] != '\\') {
			marks = append(marks, i)
		}
	}

	open, closing := -1, -1
	for i := 0; i+1 < len(marks); i += 2 {
		if p <= marks[i+1] {
			open, closing = marks[i], marks[i+1]

			break
		}
	}
	if open < 0 {
		return 0, 0, false
	}

	if !around {
		return open + 1, closing, true
	}

	to := closing + 1
	for to < end && isBlank(r[to]) {
		to++
	}
	if to == closing+1 {
		for open > start && isBlank(r[open-1]) {
			open--
		}
	}

	return open, to, true
}

func pairObject(r []rune, p int, around bool, pair [2]rune) (int, int, bool) {
	open, closing := pair[0], pair[1]
	if p >= len(r) {
		return 0, 0, false
	}

	from := p
	if r[p] != open {
		depth := 0
		if r[p] == closing {
			depth = -1
		}
		for ; from >= 0; from-- {
			switch r[from] {
			case closing:
				depth++
			case open:
				depth--
			}
			if depth < 0 {
				break
			}
		}
		if from < 0 {
			return 0, 0, false
		}
	}

	to, depth := from+1, 0
	for ; to < len(r); to++ {
		if r[to] == open {
			depth++
		}
		if r[to] == closing {
			if depth == 0 {
				break
			}
			depth--
		}
	}
	if to >= len(r) {
		return 0, 0, false
	}

	if around {
		return from, to + 1, true
	}

	return from + 1, to, true
}
