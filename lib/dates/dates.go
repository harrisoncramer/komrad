// Package dates formats timestamps as friendly, local-time labels for display.
package dates

import "time"

const friendlyLayout = "Jan 2 at 3:04pm"

// Friendly renders a stored UTC timestamp in the viewer's local time zone as
// "Jul 2 at 2:30pm", or an empty string when the time is zero.
func Friendly(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return t.Local().Format(friendlyLayout)
}
