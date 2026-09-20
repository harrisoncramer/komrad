package label

import (
	"strconv"
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

// Age is how long ago a time was, in the one unit that fits it: 40s, 5m, 3h, 2d or 6w.
func Age(t time.Time) string {
	return AgeAt(t, time.Now())
}

// AgeAt is Age measured from a given moment.
func AgeAt(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}

	since := max(now.Sub(t), 0)
	switch {
	case since < time.Minute:
		return strconv.Itoa(int(since.Seconds())) + "s"
	case since < time.Hour:
		return strconv.Itoa(int(since.Minutes())) + "m"
	case since < 24*time.Hour:
		return strconv.Itoa(int(since.Hours())) + "h"
	case since < 7*24*time.Hour:
		return strconv.Itoa(int(since.Hours()/24)) + "d"
	}

	return strconv.Itoa(int(since.Hours()/(24*7))) + "w"
}

// Day is the day a time fell on in the viewer's local time zone, "Sep 17", or nothing for a
// zero time.
func Day(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return t.Local().Format("Jan 2")
}
