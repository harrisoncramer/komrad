// Package dates turns timestamps into the labels a row shows and the RFC 3339 stamps the
// wire carries, and reads those stamps back.
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

const (
	clockLayout = "3:04pm"
	dayLayout   = "Jan 2 3:04pm"
)

// Clock is the short label a list row gets: the time of day for something that happened today,
// and the day with the time for anything older.
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
