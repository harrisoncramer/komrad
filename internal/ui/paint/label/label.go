// Package label turns a number, a time, a path, a hash or a blank into the short text a row or a
// record shows.
package label

import (
	"fmt"
	"strings"
)

// Dash is what a record shows where it has nothing to say.
const Dash = "—"

// shortSHALength is how much of a commit hash a record shows.
const shortSHALength = 12

// Bytes spells a byte count the way du does, at most four characters wide.
func Bytes(bytes int64) string {
	const unit = 1024

	value := float64(bytes)
	suffixes := []string{"B", "K", "M", "G", "T"}
	for i, suffix := range suffixes {
		if value >= 999.5 && i < len(suffixes)-1 {
			value /= unit

			continue
		}
		if value < 9.95 && suffix != "B" {
			return fmt.Sprintf("%.1f%s", value, suffix)
		}

		return fmt.Sprintf("%.0f%s", value, suffix)
	}

	return ""
}

// Count is a number with its noun, singular for one and plural otherwise.
func Count(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}

	return fmt.Sprintf("%d %s", n, plural)
}

// Counted is a number with its noun, pluralised by adding s, or es after an ending a bare s
// will not sit on.
func Counted(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}

	suffix := "s"
	for _, ending := range []string{"ch", "sh", "s", "x", "z"} {
		if strings.HasSuffix(word, ending) {
			suffix = "es"

			break
		}
	}

	return fmt.Sprintf("%d %s%s", n, word, suffix)
}

// OfCount is where in a run of things one stands, "3 of 7".
func OfCount(at, total int) string {
	return fmt.Sprintf("%d of %d", at, total)
}

// OrDash is the text as given, or a dash where it is blank.
func OrDash(text string) string {
	if strings.TrimSpace(text) == "" {
		return Dash
	}

	return text
}

// ShortSHA is a commit hash cut to the length a record names one by.
func ShortSHA(sha string) string {
	if len(sha) <= shortSHALength {
		return sha
	}

	return sha[:shortSHALength]
}

// Tally is how many more sit behind the one shown, "+4".
func Tally(n int) string {
	return fmt.Sprintf("+%d", n)
}
