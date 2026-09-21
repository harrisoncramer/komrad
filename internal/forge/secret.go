package forge

import (
	"log/slog"
	"strings"
)

// Secret is a token that prints masked wherever it is printed and gives itself up only to
// Expose.
type Secret struct {
	value string
}

// NewSecret wraps a token, trimming the newline every source of one so far has carried.
func NewSecret(value string) Secret {
	return Secret{value: strings.TrimSpace(value)}
}

// IsZero says whether there is no token at all.
func (s Secret) IsZero() bool {
	return s.value == ""
}

// Expose is the token itself, for the header that sends it and the store that keeps it.
func (s Secret) Expose() string {
	return s.value
}

// String is the token masked to its last three characters.
func (s Secret) String() string {
	return s.masked()
}

// GoString is the token masked to its last three characters.
func (s Secret) GoString() string {
	return s.masked()
}

// LogValue is the token masked to its last three characters.
func (s Secret) LogValue() slog.Value {
	return slog.StringValue(s.masked())
}

func (s Secret) masked() string {
	const mask = "*****"
	if len(s.value) < 8 {
		return mask
	}

	return mask + s.value[len(s.value)-3:]
}
