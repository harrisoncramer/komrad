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
