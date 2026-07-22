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
