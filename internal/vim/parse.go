package vim

import (
	"slices"
	"unicode/utf8"
)

const maxCount = 9999

// command is one complete normal-mode command.
type command struct {
	count   int
	op      string
	opCount int
	name    string
	char    rune
	// object says name is a text object rather than a motion.
	object bool
}

// times is the count the command runs with, one when none was typed.
func (c command) times() int {
	return min(max(c.count, 1)*max(c.opCount, 1), maxCount)
}

// given is the count typed for the command, zero when none was.
func (c command) given() int {
	if c.count == 0 && c.opCount == 0 {
		return 0
	}

	return c.times()
}

type parseStatus int

const (
	parseIncomplete parseStatus = iota
	parseInvalid
	parseComplete
)

var (
	operators = []string{"d", "c", "y"}
	simples   = []string{"x", "X", "s", "S", "~", "p", "P", "J", "D", "C", "Y", "i", "a", "I", "A", "o", "O", "u", "ctrl+r", "."}
	motions   = []string{"h", "l", "j", "k", "w", "b", "e", "W", "B", "E", "0", "^", "$", "G", ";", ",", "enter"}
	finds     = []string{"f", "F", "t", "T"}
)

var objects = map[string]string{
	"w": "w", "W": "W",
	`"`: `"`, "'": "'", "`": "`",
	"(": "(", ")": "(", "b": "(",
	"[": "[", "]": "[",
	"{": "{", "}": "{", "B": "{",
}

func parse(keys []string) (command, parseStatus) {
	var c command

	i := 0
	c.count, i = readCount(keys, i)
	if i == len(keys) {
		return c, parseIncomplete
	}

	k := normalize(keys[i])
	i++

	switch {
	case slices.Contains(operators, k):
		c.op = k

		c.opCount, i = readCount(keys, i)
		if i == len(keys) {
			return c, parseIncomplete
		}

		next := normalize(keys[i])
		i++

		switch next {
		case k:
			c.name = "line"

			return c, parseComplete
		case "i", "a":
			if i == len(keys) {
				return c, parseIncomplete
			}

			object, ok := objects[keys[i]]
			if !ok {
				return c, parseInvalid
			}
			c.name, c.object = next+object, true

			return c, parseComplete
		}

		return parseMotion(c, next, keys, i)
	case k == "r":
		if i == len(keys) {
			return c, parseIncomplete
		}

		char, ok := keyChar(keys[i])
		if !ok {
			return c, parseInvalid
		}
		c.name, c.char = k, char

		return c, parseComplete
	case slices.Contains(simples, k):
		c.name = k

		return c, parseComplete
	}

	return parseMotion(c, k, keys, i)
}

func parseMotion(c command, k string, keys []string, i int) (command, parseStatus) {
	switch {
	case slices.Contains(motions, k):
		c.name = k

		return c, parseComplete
	case k == "g":
		if i == len(keys) {
			return c, parseIncomplete
		}
		if keys[i] != "g" {
			return c, parseInvalid
		}
		c.name = "gg"

		return c, parseComplete
	case slices.Contains(finds, k):
		if i == len(keys) {
			return c, parseIncomplete
		}

		char, ok := keyChar(keys[i])
		if !ok {
			return c, parseInvalid
		}
		c.name, c.char = k, char

		return c, parseComplete
	}

	return c, parseInvalid
}

func readCount(keys []string, i int) (int, int) {
	n := 0
	for i < len(keys) {
		k := keys[i]
		if len(k) != 1 || k[0] < '0' || k[0] > '9' || (n == 0 && k[0] == '0') {
			break
		}
		n = min(n*10+int(k[0]-'0'), maxCount)
		i++
	}

	return n, i
}

// awaitsChar reports whether the next key is read as a character rather than a command.
func awaitsChar(keys []string) bool {
	if len(keys) == 0 {
		return false
	}

	_, i := readCount(keys, 0)
	rest := keys[i:]
	if len(rest) > 0 && slices.Contains(operators, rest[0]) {
		_, j := readCount(rest, 1)
		rest = rest[j:]
		if len(rest) == 1 && (rest[0] == "i" || rest[0] == "a") {
			return true
		}
	}

	return len(rest) == 1 && (rest[0] == "r" || slices.Contains(finds, rest[0]))
}

func normalize(k string) string {
	switch k {
	case "left", "backspace":
		return "h"
	case "right", "space":
		return "l"
	case "up":
		return "k"
	case "down":
		return "j"
	case "delete":
		return "x"
	}

	return k
}

// keyChar is the character a key types, if it types one.
func keyChar(k string) (rune, bool) {
	if k == "space" {
		return ' ', true
	}

	c, size := utf8.DecodeRuneInString(k)
	if c == utf8.RuneError || size != len(k) {
		return 0, false
	}

	return c, true
}
