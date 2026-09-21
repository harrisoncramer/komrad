package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestShortSHA covers a full hash being cut and a short one left alone.
func TestShortSHA(t *testing.T) {
	tests := []struct {
		sha  string
		want string
	}{
		{sha: "", want: ""},
		{sha: "abc123", want: "abc123"},
		{sha: "0123456789abcdef0123456789abcdef01234567", want: "0123456789ab"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, ShortSHA(tt.sha))
		})
	}
}
