package dates

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestStamp pins what the wire carries, where a zero time reads as no timestamp rather than as
// the year one.
func TestStamp(t *testing.T) {
	assert.Empty(t, Stamp(time.Time{}))
	assert.Equal(t, "2026-09-05T14:30:00Z", Stamp(time.Date(2026, 9, 5, 14, 30, 0, 0, time.UTC)))
}

// TestStampPtr pins that an absent time stays absent.
func TestStampPtr(t *testing.T) {
	assert.Nil(t, StampPtr(nil))

	at := time.Date(2026, 9, 5, 14, 30, 0, 0, time.UTC)
	stamped := StampPtr(&at)
	if assert.NotNil(t, stamped) {
		assert.Equal(t, "2026-09-05T14:30:00Z", *stamped)
	}
}

// TestPreciseStamp pins that a fraction of a second survives.
func TestPreciseStamp(t *testing.T) {
	assert.Empty(t, PreciseStamp(time.Time{}))
	assert.Equal(t, "2026-09-05T14:30:00.25Z", PreciseStamp(time.Date(2026, 9, 5, 14, 30, 0, 250_000_000, time.UTC)))
	assert.Equal(t, time.Date(2026, 9, 5, 14, 30, 0, 250_000_000, time.UTC), Parse("2026-09-05T14:30:00.25Z").UTC())
}

// TestPreciseStampPtr pins that an absent time stays absent.
func TestPreciseStampPtr(t *testing.T) {
	assert.Nil(t, PreciseStampPtr(nil))

	at := time.Date(2026, 9, 5, 14, 30, 0, 250_000_000, time.UTC)
	stamped := PreciseStampPtr(&at)
	if assert.NotNil(t, stamped) {
		assert.Equal(t, "2026-09-05T14:30:00.25Z", *stamped)
	}
}

// TestParse pins that a stamp komrad cannot read comes back as the zero time rather than as an
// error the caller would have nothing to do with.
func TestParse(t *testing.T) {
	assert.True(t, Parse("").IsZero())
	assert.True(t, Parse("not a time").IsZero())
	assert.Equal(t, time.Date(2026, 9, 5, 14, 30, 0, 0, time.UTC), Parse("2026-09-05T14:30:00Z").UTC())
}

// TestParsePtr pins that an absent or unreadable stamp comes back as nothing.
func TestParsePtr(t *testing.T) {
	assert.Nil(t, ParsePtr(""))
	assert.Nil(t, ParsePtr("not a time"))

	parsed := ParsePtr("2026-09-05T14:30:00Z")
	if assert.NotNil(t, parsed) {
		assert.Equal(t, time.Date(2026, 9, 5, 14, 30, 0, 0, time.UTC), parsed.UTC())
	}
}

// TestBetween pins the units chosen at each size, an unset end reading as no duration, and an end
// before its start reading as none rather than below zero.
func TestBetween(t *testing.T) {
	start := time.Date(2026, 9, 5, 14, 30, 0, 0, time.UTC)

	tests := []struct {
		name  string
		start time.Time
		end   time.Time
		want  string
	}{
		{
			name:  "seconds",
			start: start,
			end:   start.Add(42 * time.Second),
			want:  "42s",
		},
		{
			name:  "minutes and seconds",
			start: start,
			end:   start.Add(3*time.Minute + 8*time.Second),
			want:  "3m 08s",
		},
		{
			name:  "hours and minutes",
			start: start,
			end:   start.Add(time.Hour + 5*time.Minute + 30*time.Second),
			want:  "1h 05m",
		},
		{
			name:  "rounded to the second",
			start: start,
			end:   start.Add(1500 * time.Millisecond),
			want:  "2s",
		},
		{
			name:  "end before start",
			start: start,
			end:   start.Add(-time.Second),
			want:  "0s",
		},
		{
			name:  "unset start",
			start: time.Time{},
			end:   start,
			want:  "",
		},
		{
			name:  "unset end",
			start: start,
			end:   time.Time{},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Between(tt.start, tt.end))
		})
	}
}
