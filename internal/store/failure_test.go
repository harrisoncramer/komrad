package store

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

// TestFailureReasonIsSomethingARowCanHold covers the cut and the scrub, with Postgres refusing
// a text column that holds a NUL or half a rune.
func TestFailureReasonIsSomethingARowCanHold(t *testing.T) {
	tests := []struct {
		name     string
		cause    error
		contains string
	}{
		{
			name:  "nothing at all",
			cause: nil,
		},
		{
			name:     "a reason short enough to keep whole",
			cause:    errors.New("a hook failed: install-deps: ✓ done, then exit 3"),
			contains: "✓ done",
		},
		{
			name:  "a cut that would land inside a rune",
			cause: errors.New(strings.Repeat("a", maxFailureReasonLen-2) + "✓✓"),
		},
		{
			name:     "output carrying a NUL",
			cause:    errors.New("a hook failed:\x00 install-deps"),
			contains: "install-deps",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := FailureReason(tt.cause)

			assert.True(t, utf8.ValidString(reason), "Postgres refuses a text column that is not valid UTF-8")
			assert.NotContains(t, reason, "\x00", "and refuses one holding a NUL")
			assert.LessOrEqual(t, len(reason), maxFailureReasonLen)
			if tt.contains != "" {
				assert.Contains(t, reason, tt.contains)
			}
		})
	}
}
