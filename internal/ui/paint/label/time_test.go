package label

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestFriendly pins how each time renders for a person reading the panel, a zero time
// included.
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
		{
			name: "a UTC instant is rendered in local time, not left in UTC",
			in:   utcAfternoon,
			want: utcAfternoon.Local().Format(friendlyLayout),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Friendly(tt.in))
		})
	}
}

// utcAfternoon is an instant fixed in UTC, so the case built on it says
// something about the conversion rather than about the machine's zone.
var utcAfternoon = time.Date(2024, time.July, 2, 14, 30, 0, 0, time.UTC)

// TestAgeAtPicksTheOneUnitThatFits pins the unit chosen at each boundary, and a future time
// reading as no age at all rather than a negative one.
func TestAgeAtPicksTheOneUnitThatFits(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		at   time.Time
		want string
	}{
		{
			name: "seconds",
			at:   now.Add(-40 * time.Second),
			want: "40s",
		},
		{
			name: "minutes",
			at:   now.Add(-5 * time.Minute),
			want: "5m",
		},
		{
			name: "hours",
			at:   now.Add(-3 * time.Hour),
			want: "3h",
		},
		{
			name: "days",
			at:   now.Add(-50 * time.Hour),
			want: "2d",
		},
		{
			name: "weeks",
			at:   now.Add(-45 * 24 * time.Hour),
			want: "6w",
		},
		{
			name: "ahead of now",
			at:   now.Add(time.Hour),
			want: "0s",
		},
		{name: "zero", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, AgeAt(tt.at, now))
		})
	}
}

// TestDurationPicksTheUnitsThatFit pins the units chosen at each size, and a negative span reading as
// none rather than below zero.
func TestDurationPicksTheUnitsThatFit(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{
			name: "seconds",
			d:    42 * time.Second,
			want: "42s",
		},
		{
			name: "minutes and seconds",
			d:    3*time.Minute + 8*time.Second,
			want: "3m 08s",
		},
		{
			name: "hours and minutes",
			d:    time.Hour + 5*time.Minute + 30*time.Second,
			want: "1h 05m",
		},
		{
			name: "rounded to the second",
			d:    1500 * time.Millisecond,
			want: "2s",
		},
		{
			name: "negative",
			d:    -time.Second,
			want: "0s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Duration(tt.d))
		})
	}
}
