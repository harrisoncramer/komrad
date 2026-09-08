package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSanitizeName pins which characters are rewritten to make a tmux window name.
func TestSanitizeName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "colon becomes a dash",
			in:   "a:b",
			want: "a-b",
		},
		{
			name: "dot becomes a dash",
			in:   "v1.0",
			want: "v1-0",
		},
		{
			name: "space becomes a dash",
			in:   "my agent",
			want: "my-agent",
		},
		{
			name: "a mix is fully sanitized",
			in:   "My Agent: v1.0",
			want: "My-Agent--v1-0",
		},
		{
			name: "an already-clean name is unchanged",
			in:   "clean-name",
			want: "clean-name",
		},
		{
			name: "a newline becomes a dash",
			in:   "ab\ncd",
			want: "ab-cd",
		},
		{
			name: "a delete becomes a dash",
			in:   "ab\x7fcd",
			want: "ab-cd",
		},
		{
			name: "characters above the ascii control range are kept",
			in:   "caf\u00e9\u00a0",
			want: "caf\u00e9\u00a0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, SanitizeName(tt.in))
		})
	}
}
