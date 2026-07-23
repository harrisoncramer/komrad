// Package dates formats timestamps as friendly, local-time labels for display.
package dates

import "time"

// friendlyLayout renders a time like "Jul 2 at 2:30pm": abbreviated month, day
// with no leading zero, and a lowercase 12-hour clock.
const friendlyLayout = "Jan 2 at 3:04pm"

// Friendly renders a timestamp in the viewer's local time zone as
// "Jul 2 at 2:30pm". Stored timestamps are UTC, so it converts first. A zero
// time yields an empty string, so callers can render "unknown" as blank.
func Friendly(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return t.Local().Format(friendlyLayout)
}
