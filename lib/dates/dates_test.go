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
