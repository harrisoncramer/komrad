package screen_test

import (
	"testing"

	"komrad/internal/ui/screen"

	"github.com/stretchr/testify/assert"
)

// TestString covers how an error names each screen.
func TestString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   screen.ID
		want string
	}{
		{
			name: "a screen the panel does not have",
			id:   screen.ID(-1),
			want: "",
		},
		{
			name: "the agents list",
			id:   screen.Agents,
			want: "the agents list",
		},
		{
			name: "the status check logs",
			id:   screen.StatusCheckLogs,
			want: "the status check logs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.id.String())
		})
	}
}

// TestIDsAreNamed covers every screen the panel lists having a name of its own.
func TestIDsAreNamed(t *testing.T) {
	t.Parallel()

	seen := map[string]screen.ID{}
	for _, id := range screen.IDs {
		name := id.String()
		assert.NotEmpty(t, name, "screen %d", id)
		assert.NotContains(t, seen, name, "screen %d", id)
		seen[name] = id
	}
}
