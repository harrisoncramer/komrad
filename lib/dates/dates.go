// Package dates formats timestamps as friendly, local-time labels for display.
package dates

import (
	"fmt"
	"time"
)

const friendlyLayout = "Jan 2 at 3:04pm"

// Friendly renders a stored UTC timestamp in the viewer's local time zone as
// "Jul 2 at 2:30pm", or an empty string when the time is zero.
func Friendly(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return t.Local().Format(friendlyLayout)
}

// Elapsed renders a span as the coarsest unit that still says something, so a row shows "2m"
// rather than a count of seconds that would be stale by the time it is read.
func Elapsed(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", max(int(d.Seconds()), 0))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	}
}

const (
	clockLayout = "3:04pm"
	dayLayout   = "Jan 2"
)

// Clock is the short label a list row gets: the time of day for something that happened today,
// and the day itself for anything older, since a bare clock on last week's row reads as though
// it had just arrived.
func Clock(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	local := t.Local()
	if now := time.Now().Local(); local.YearDay() == now.YearDay() && local.Year() == now.Year() {
		return local.Format(clockLayout)
	}

	return local.Format(dayLayout)
}
