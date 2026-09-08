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

// TestClock pins the two shapes a row label takes, today and last week not reading the same.
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
			name: "something older is the day it happened and the time it happened at",
			in:   now.AddDate(0, 0, -8),
			want: now.AddDate(0, 0, -8).Format("Jan 2 3:04pm"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Clock(tt.in))
		})
	}
}

// TestClockReadsAnInstantInLocalTime pins the conversion against a zone of its own, on an instant
// late enough in the UTC day that the two zones disagree about which day it is.
func TestClockReadsAnInstantInLocalTime(t *testing.T) {
	here := time.Local
	t.Cleanup(func() { time.Local = here })

	// 23:30 UTC on the second is already the third in this zone, which is the whole of what the
	// case is asking.
	time.Local = time.FixedZone("test/+12", 12*60*60)

	assert.Equal(t, "Jul 3 11:30am", Clock(time.Date(2024, time.July, 2, 23, 30, 0, 0, time.UTC)))
}

// TestStamp pins what the wire carries, where a zero time reads as no timestamp rather than as
// the year one.
func TestStamp(t *testing.T) {
	assert.Empty(t, Stamp(time.Time{}))
	assert.Equal(t, "2026-09-05T14:30:00Z", Stamp(time.Date(2026, 9, 5, 14, 30, 0, 0, time.UTC)))
}

// TestStampPtr pins that an absent time stays absent, which is what a field the caller may
// leave unset needs.
func TestStampPtr(t *testing.T) {
	assert.Nil(t, StampPtr(nil))

	at := time.Date(2026, 9, 5, 14, 30, 0, 0, time.UTC)
	stamped := StampPtr(&at)
	if assert.NotNil(t, stamped) {
		assert.Equal(t, "2026-09-05T14:30:00Z", *stamped)
	}
}

// TestParse pins that a stamp weave cannot read comes back as the zero time rather than as an
// error the caller would have nothing to do with.
func TestParse(t *testing.T) {
	assert.True(t, Parse("").IsZero())
	assert.True(t, Parse("not a time").IsZero())
	assert.Equal(t, time.Date(2026, 9, 5, 14, 30, 0, 0, time.UTC), Parse("2026-09-05T14:30:00Z").UTC())
}

// TestParsePtr pins that an absent or unreadable stamp comes back as nothing, which is how an
// optional timestamp stays optional across the wire.
func TestParsePtr(t *testing.T) {
	assert.Nil(t, ParsePtr(""))
	assert.Nil(t, ParsePtr("not a time"))

	parsed := ParsePtr("2026-09-05T14:30:00Z")
	if assert.NotNil(t, parsed) {
		assert.Equal(t, time.Date(2026, 9, 5, 14, 30, 0, 0, time.UTC), parsed.UTC())
	}
}
