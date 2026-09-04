package dates

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

// TestElapsed pins the unit each span reads in, since the panel shows one short label and the
// boundaries between units are where it would otherwise say something misleading.
func TestElapsed(t *testing.T) {
	tests := []struct {
		name string
		in   time.Duration
		want string
	}{
		{
			name: "a backwards clock reads as no time at all",
			in:   -5 * time.Second,
			want: "0s",
		},
		{
			name: "a span under a minute reads in seconds",
			in:   43 * time.Second,
			want: "43s",
		},
		{
			name: "a minute exactly reads in minutes",
			in:   time.Minute,
			want: "1m",
		},
		{
			name: "a span under an hour rounds down to minutes",
			in:   59*time.Minute + 59*time.Second,
			want: "59m",
		},
		{
			name: "an hour exactly reads in hours",
			in:   time.Hour,
			want: "1h",
		},
		{
			name: "a span under a day rounds down to hours",
			in:   23*time.Hour + 59*time.Minute,
			want: "23h",
		},
		{
			name: "a day and a half reads as one day",
			in:   36 * time.Hour,
			want: "1d",
		},
		{
			name: "a long span reads in days",
			in:   9 * 24 * time.Hour,
			want: "9d",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Elapsed(tt.in))
		})
	}
}

// TestClock pins the two shapes a row label takes, since the point of it is that today and
// last week do not read the same.
func TestClock(t *testing.T) {
	now := time.Now().Local()

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
			name: "something today is the time of day",
			in:   time.Date(now.Year(), now.Month(), now.Day(), 14, 30, 0, 0, time.Local),
			want: "2:30pm",
		},
		{
			name: "something older is the day it happened",
			in:   now.AddDate(0, 0, -8),
			want: now.AddDate(0, 0, -8).Format("Jan 2"),
		},
		{
			name: "a UTC instant is read in local time, not left in UTC",
			in:   utcAfternoon,
			want: dayOrClock(utcAfternoon.Local()),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Clock(tt.in))
		})
	}
}

func dayOrClock(t time.Time) string {
	now := time.Now().Local()
	if t.YearDay() == now.YearDay() && t.Year() == now.Year() {
		return t.Format("3:04pm")
	}

	return t.Format("Jan 2")
}
