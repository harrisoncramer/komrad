package engine_test

import (
	"testing"

	"komrad/internal/engine"

	"github.com/stretchr/testify/assert"
)

// TestIsRemoteIsWhetherAURLWasGiven covers the zero value meaning a local engine.
func TestIsRemoteIsWhetherAURLWasGiven(t *testing.T) {
	t.Parallel()

	assert.False(t, engine.Remote{}.IsRemote())
	assert.True(t, engine.Remote{URL: "https://komrad.example.com"}.IsRemote())
}

// TestStringNamesTheEngine covers what a log line or an error says about where agents are.
func TestStringNamesTheEngine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		remote engine.Remote
		want   string
	}{
		{
			name:   "a local engine",
			remote: engine.Remote{},
			want:   "local",
		},
		{
			name: "an engine named by its url",
			remote: engine.Remote{
				URL: "https://komrad.example.com",
			},
			want: "https://komrad.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.remote.String())
		})
	}
}
