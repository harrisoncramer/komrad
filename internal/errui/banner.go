package errui

import (
	"errors"
	"strings"

	"connectrpc.com/connect"
)

// Banner is a write the panel asked for and the service refused.
//
// It reports without taking the screen, unlike the screens above. Those mean
// nothing on screen can be trusted. A refused action means one thing did not
// happen, and the list it did not happen to is the context the error needs.
//
// The caller is expected to hold it until dismissed rather than show it and forget
// it, because the panel reloads its lists after every action. Without it a refused
// delete is indistinguishable from a keypress that never registered: the row is
// still there either way.
type Banner struct {
	// Summary says what the panel was trying to do, in the terms the user acted in
	// rather than the service's.
	Summary string
	// Detail is the reason the service gave.
	Detail string
}

// bannerIndent lines the detail up under the summary, past the marker.
const bannerIndent = "  "

// Render draws the banner that sits above the active screen.
func (b Banner) Render() string {
	var out strings.Builder

	out.WriteString(Red.Render("✗ "+b.Summary) + "\n")

	// A service that fails without saying why still has to render as something, or
	// the banner is a red line with nothing under it.
	detail := b.Detail
	if detail == "" {
		detail = "no reason given"
	}

	out.WriteString(bannerIndent + detail + "\n")
	out.WriteString(Dim.Render(bannerIndent+"esc to dismiss") + "\n\n")

	return out.String()
}

// Reason reduces a service error to the part worth reading. A connect error
// stringifies with its code in front of the message the service sent, and the code
// says less to the person reading it than the message does.
func Reason(err error) string {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Message()
	}

	return err.Error()
}
