package errui

import (
	"cmp"
	"errors"
	"strings"

	"weave/internal/caller"

	"connectrpc.com/connect"
)

type Banner struct {
	Summary string
	Detail  string
	Hint    string
	Trace   string
}

const bannerIndent = "  "

// Render draws the banner above the active screen folded to a pane of the given width, where
// zero means the panel has not been told its size yet.
func (b Banner) Render(width int) string {
	folded := foldWidth(width)

	var out strings.Builder

	out.WriteString(styled(Red, fold("✗ "+b.Summary, folded, bannerIndent)) + "\n")

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

// GetTrace is the id the service logged a failure under, which it returns on the error's
// metadata, and blank for a failure that never reached the service or came back without one.
func GetTrace(err error) string {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Meta().Get(caller.TraceHeader)
	}

	return ""
}

// DidSetupRun reports whether the failure was a script weave ran on the way in, running and exiting
// non-zero, which is the one failure whose output the caller is already looking at.
func DidSetupRun(err error) bool {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Meta().Get(caller.SetupRanHeader) == "true"
	}

	return false
}

// GetReason reduces a service error to the part worth reading: a connect error stringifies
// with its code in front of the message, and the code says less to a person reading it
// than the message does.
func GetReason(err error) string {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Message()
	}

	return err.Error()
}
