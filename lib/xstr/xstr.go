// Package xstr holds small generic string helpers.
package xstr

import "strings"

// FirstLine returns the first line of s, without its trailing newline.
func FirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}

	return s
}

// Truncate shortens s to at most maxLen runes, appending an ellipsis when cut.
func Truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}

	if maxLen <= 1 {
		return string(runes[:maxLen])
	}

	return string(runes[:maxLen-1]) + "…"
}

// Slug turns s into a lowercase dash-separated token of at most maxLen runes,
// keeping only letters and digits. It returns "" when nothing usable remains.
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
