package oneline

import "sort"

type counter struct{ n int }

func (c counter) value() int { return c.n } // want `value is declared on one line: put its body on lines of its own`

func (c *counter) add(n int) { c.n += n } // want `add is declared on one line: put its body on lines of its own`

func one() int { return 1 } // want `one is declared on one line: put its body on lines of its own`

func nothing() {}

func two() int {
	return 2
}

func sorted(values []int) []int {
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })

	return values
}

var pick = func() int { return 3 }
