// Package dates turns a time into the RFC 3339 stamp the wire carries, reads one back, and says how long passed between two.
package dates

import (
	"fmt"
	"time"
)

// Stamp renders a time as the wire spells it, an unset one as an empty string.
func Stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return t.Format(time.RFC3339)
}

// StampPtr renders a time that may not be there, leaving an absent one absent.
func StampPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}

	stamped := t.Format(time.RFC3339)

	return &stamped
}

// PreciseStamp renders a time as Stamp does but to the nanosecond.
func PreciseStamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return t.Format(time.RFC3339Nano)
}

// PreciseStampPtr renders a time that may not be there to the nanosecond, leaving an absent one
// absent.
func PreciseStampPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}

	stamped := t.Format(time.RFC3339Nano)

	return &stamped
}

// Parse reads a wire stamp, giving back the zero time for anything that will not parse.
func Parse(stamp string) time.Time {
	parsed, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return time.Time{}
	}

	return parsed
}

// ParsePtr reads a wire stamp that may not be there, giving back nothing for an absent or
// unreadable one.
func ParsePtr(stamp string) *time.Time {
	parsed, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return nil
	}

	return &parsed
}

// Between is how long passed from start to end as 42s, 3m 08s or 1h 05m, empty when either is unset.
func Between(start, end time.Time) string {
	if start.IsZero() || end.IsZero() {
		return ""
	}

	d := max(end.Sub(start), 0).Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm %02ds", int(d.Minutes()), int(d.Seconds())%60)
	}

	return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
}
