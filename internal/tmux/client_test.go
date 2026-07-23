package tmux

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClientTarget(t *testing.T) {
	tests := []struct {
		name   string
		client string
		want   []string
	}{
		{
			name:   "an empty client yields no target flags",
			client: "",
			want:   nil,
		},
		{
			name:   "a named client yields a -c target",
			client: "/dev/ttys001",
			want:   []string{"-c", "/dev/ttys001"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, clientTarget(tt.client))
		})
	}
}
