package dates

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFriendly(t *testing.T) {
	tests := []struct {
		name string
		in   time.Time
		want string
	}{
		{
			name: "a zero time renders empty",
			in:   time.Time{},
			want: "",
		},
		{
			name: "an afternoon time reads as a friendly label",
			in:   time.Date(2024, time.July, 2, 14, 30, 0, 0, time.Local),
			want: "Jul 2 at 2:30pm",
		},
		{
			name: "a morning time uses am",
			in:   time.Date(2024, time.December, 15, 9, 5, 0, 0, time.Local),
			want: "Dec 15 at 9:05am",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Friendly(tt.in))
		})
	}
}

// TestFriendlyConvertsToLocal confirms a UTC instant is rendered in local time,
// not left in UTC.
func TestFriendlyConvertsToLocal(t *testing.T) {
	utc := time.Date(2024, time.July, 2, 14, 30, 0, 0, time.UTC)

	assert.Equal(t, utc.Local().Format(friendlyLayout), Friendly(utc))
}
