package errui

import (
	"cmp"
	"errors"
	"strings"

	"weave/internal/caller"

	"connectrpc.com/connect"
)

// Banner is a write the panel asked for and the service refused. Unlike the screens above,
// it reports without taking the screen, since a refused write means one thing didn't
// happen rather than putting everything on screen in doubt.
//
// The caller is expected to hold it until dismissed rather than show it and forget it,
// since the panel reloads its lists after every write and a refused delete would otherwise
// look identical to a keypress that never registered.
type Banner struct {
	// Summary says what the panel was trying to do, in the terms the user acted in
	// rather than the service's.
	Summary string
	// Detail is the reason the service gave.
	Detail string
	// Hint says how the banner goes away: blank means esc, except over a form, where esc
	// backs out of the form instead.
	Hint string
	// Trace is the id the service logged this failure under, blank when it did not say.
	// It is offered as the command that reads it rather than as a bare id, since knowing
	// the id and knowing what to do with it are not the same thing.
	Trace string
}

// bannerIndent lines the detail up under the summary, past the marker.
const bannerIndent = "  "

// Render draws the banner that sits above the active screen, folded to a pane of the given
// width; zero means the panel hasn't been told its size yet. The reason the service gave is
// the whole point of the banner and can run as long as the service made it, so it's folded
// rather than cut at the pane edge.
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
	hint := cmp.Or(b.Hint, "esc to dismiss")
	if b.Trace != "" {
		hint += "  ·  weave trace " + b.Trace
	}

	out.WriteString(Dim.Render(bannerIndent+hint) + "\n\n")

	return out.String()
}

// TraceOf is the id the service logged a failure under, which it returns on the error's
// metadata, and blank for a failure that never reached the service or came back without one.
func TraceOf(err error) string {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Meta().Get(caller.TraceHeader)
	}

	return ""
}

// Reason reduces a service error to the part worth reading: a connect error stringifies
// with its code in front of the message, and the code says less to a person reading it
// than the message does.
func Reason(err error) string {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Message()
	}

	return err.Error()
}
