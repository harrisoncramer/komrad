// Package xstr holds small generic string helpers.
package xstr

import (
	"strings"
	"unicode/utf8"
)

// FirstLine returns the first line of s, without its trailing newline.
func FirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}

	return s
}

// Slug turns s into a lowercase dash-separated token of at most maxLen runes, keeping only
// letters and digits and returning "" when nothing usable remains.
func Slug(s string, maxLen int) string {
	var b strings.Builder
	lastDash := true // avoid a leading dash
	for _, r := range strings.ToLower(s) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteRune('-')
			lastDash = true
		}

		if b.Len() >= maxLen {
			break
		}
	}

	return strings.Trim(b.String(), "-")
}

// Head returns the first limit bytes of s and whether anything was cut, backing off a byte at
// a time so the cut never lands inside a rune.
func Head(s string, limit int) (string, bool) {
	if len(s) <= limit {
		return s, false
	}

	cut := s[:limit]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}

	return cut, true
}

// Tail returns the last limit bytes of s and whether anything was cut, moving the cut forward a
// byte at a time so it never lands inside a rune.
func Tail(s string, limit int) (string, bool) {
	if len(s) <= limit {
		return s, false
	}

	cut := s[len(s)-limit:]
	for len(cut) > 0 && !utf8.RuneStart(cut[0]) {
		cut = cut[1:]
	}

	return cut, true
}
