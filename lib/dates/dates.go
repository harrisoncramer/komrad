package dates

import (
	"fmt"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// Stamp renders a time as the wire carries it, an unset one as no timestamp.
func Stamp(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}

	return timestamppb.New(t)
}

// StampPtr renders a time that may not be there, leaving an absent one absent.
func StampPtr(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}

	return timestamppb.New(*t)
}

// Read reads a wire timestamp, giving back the zero time for an unset one.
func Read(stamp *timestamppb.Timestamp) time.Time {
	if stamp == nil {
		return time.Time{}
	}

	return stamp.AsTime()
}

// ReadPtr reads a wire timestamp that may not be there, giving back nothing for an unset one.
func ReadPtr(stamp *timestamppb.Timestamp) *time.Time {
	if stamp == nil {
		return nil
	}

	read := stamp.AsTime()

	return &read
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
