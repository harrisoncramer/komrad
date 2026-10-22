package dates

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestStamp pins what the wire carries, where a zero time reads as no timestamp rather than as
// the year one, and a fraction of a second survives.
func TestStamp(t *testing.T) {
	assert.Nil(t, Stamp(time.Time{}))

	at := time.Date(2026, 9, 5, 14, 30, 0, 250_000_000, time.UTC)
	assert.Equal(t, at, Stamp(at).AsTime())
}

// TestStampPtr pins that an absent time stays absent.
func TestStampPtr(t *testing.T) {
	assert.Nil(t, StampPtr(nil))

	at := time.Date(2026, 9, 5, 14, 30, 0, 0, time.UTC)
	assert.Equal(t, at, StampPtr(&at).AsTime())
}

// TestRead pins that an unset timestamp comes back as the zero time rather than as the start of
// 1970.
func TestRead(t *testing.T) {
	assert.True(t, Read(nil).IsZero())

	at := time.Date(2026, 9, 5, 14, 30, 0, 250_000_000, time.UTC)
	assert.Equal(t, at, Read(timestamppb.New(at)))
}

// TestReadPtr pins that an unset timestamp comes back as nothing.
func TestReadPtr(t *testing.T) {
	assert.Nil(t, ReadPtr(nil))

	at := time.Date(2026, 9, 5, 14, 30, 0, 0, time.UTC)
	read := ReadPtr(timestamppb.New(at))
	if assert.NotNil(t, read) {
		assert.Equal(t, at, *read)
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
