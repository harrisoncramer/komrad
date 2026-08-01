package errui

import (
	"cmp"
	"errors"
	"strings"

	"connectrpc.com/connect"
)

// Banner is a write the panel asked for and the service refused.
//
// It reports without taking the screen, unlike the screens above. Those mean
// nothing on screen can be trusted. A refused write means one thing did not
// happen, and the list it did not happen to is the context the error needs.
//
// The caller is expected to hold it until dismissed rather than show it and forget
// it, because the panel reloads its lists after every write. Without it a refused
// delete is indistinguishable from a keypress that never registered: the row is
// still there either way.
type Banner struct {
	// Summary says what the panel was trying to do, in the terms the user acted in
	// rather than the service's.
	Summary string
	// Detail is the reason the service gave.
	Detail string
	// Hint is the line saying how the banner goes away, blank for the esc that dismisses
	// most of them. A banner over a form sets it, since esc there backs out of the form.
	Hint string
}

// defaultHint is how a banner goes away when nothing else owns esc.
const defaultHint = "esc to dismiss"

// bannerIndent lines the detail up under the summary, past the marker.
const bannerIndent = "  "

// Render draws the banner that sits above the active screen, folded to a pane of
// the given width. A width of zero means the panel has not been told its size yet.
//
// The reason the service gave is the whole point of the banner and is as long as the
// service made it, so it is folded rather than left to be cut at the pane edge.
func (b Banner) Render(width int) string {
	folded := foldWidth(width)

	var out strings.Builder

	out.WriteString(styled(Red, fold("✗ "+b.Summary, folded, bannerIndent)) + "\n")

	// A service that fails without saying why still has to render as something, or
	// the banner is a red line with nothing under it.
	detail := b.Detail
	if detail == "" {
		detail = "no reason given"
	}

	out.WriteString(bannerIndent + fold(detail, folded, bannerIndent) + "\n")
	out.WriteString(Dim.Render(bannerIndent+cmp.Or(b.Hint, defaultHint)) + "\n\n")

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
