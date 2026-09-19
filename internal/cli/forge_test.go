package cli

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDescribeVerifiedAt covers the verified column, which shows a person the stamp in their
// own time zone and falls back to the wire's spelling for one it cannot read.
func TestDescribeVerifiedAt(t *testing.T) {
	t.Parallel()

	instant, err := time.Parse(time.RFC3339, "2026-09-18T21:47:20Z")
	require.NoError(t, err)

	tests := []struct {
		name  string
		stamp string
		want  string
	}{
		{
			name:  "a stamp that will not parse is shown as it came",
			stamp: "yesterday",
			want:  "yesterday",
		},
		{
			name:  "a wire stamp is shown in local time to the minute",
			stamp: "2026-09-18T21:47:20Z",
			want:  instant.Local().Format("2006-01-02 15:04"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, describeVerifiedAt(tt.stamp))
		})
	}
}
