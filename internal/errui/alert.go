package errui

import (
	"cmp"
	"errors"
	"strings"

	"connectrpc.com/connect"
	"github.com/charmbracelet/lipgloss"
)

// Level is how urgently an alert needs reading, and what closes it. It is the zero value on
// an Alert built without naming one, which is why LevelError sits first: a caller that never
// had to think about levels was always reporting a refused write.
type Level int

const (
	// LevelError is a write the panel asked for and the service refused. It stays until esc
	// dismisses it, since it is the one thing a user must not miss.
	LevelError Level = iota
	// LevelWarn is worth knowing but not a failure the panel caused. It stays until
	// dismissed, the way an error does.
	LevelWarn
	// LevelInfo says something the panel was not asked about went as it should. It expires
	// on its own, since there is nothing here to act on.
	LevelInfo
)

// Marker is the glyph an alert's level draws in front of its summary.
func (l Level) Marker() string {
	switch l {
	case LevelWarn:
		return "⚠"
	case LevelInfo:
		return "i"
	default:
		return "✗"
	}
}

// Style is the colour an alert's level draws its summary in.
func (l Level) Style() lipgloss.Style {
	switch l {
	case LevelWarn:
		return Yellow
	case LevelInfo:
		return Blue
	default:
		return Red
	}
}

// Alert is one thing the panel wants read: a write the service refused, or something worth
// knowing that nobody asked for.
//
// It reports without taking the screen, unlike the screens above. Those mean nothing on
// screen can be trusted. An alert means one thing happened, and the list it happened to is
// the context it needs.
//
// A caller reporting a refused write is expected to hold it until dismissed rather than show
// it and forget it, because the panel reloads its lists after every write. Without it a
// refused delete is indistinguishable from a keypress that never registered: the row is
// still there either way.
type Alert struct {
	// Level is how urgently this needs reading, which chooses its marker, its colour, and
	// how long it lasts.
	Level Level
	// Summary says what happened, in the terms the user acted in rather than the
	// service's.
	Summary string
	// Detail is the reason the service gave, or what happened, in more words than the
	// summary holds.
	Detail string
	// Hint says how the alert goes away. Blank means esc, which is every alert but one over
	// a form, where esc backs out of the form.
	Hint string
}

// alertIndent lines the detail up under the summary, past the marker.
const alertIndent = "  "

// Render draws the alert that sits above the active screen, folded to a pane of the given
// width. A width of zero means the panel has not been told its size yet.
//
// The reason the service gave is the whole point of an error alert and is as long as the
// service made it, so it is folded rather than left to be cut at the pane edge.
func (a Alert) Render(width int) string {
	folded := foldWidth(width)
	style := a.Level.Style()

	var out strings.Builder

	out.WriteString(styled(style, fold(a.Level.Marker()+" "+a.Summary, folded, alertIndent)) + "\n")

	// An alert with nothing to say beyond its summary still has to render as something, or
	// it is a coloured line with nothing under it.
	detail := a.Detail
	if detail == "" {
		detail = "no reason given"
	}

	out.WriteString(alertIndent + fold(detail, folded, alertIndent) + "\n")
	out.WriteString(Dim.Render(alertIndent+cmp.Or(a.Hint, "esc to dismiss")) + "\n\n")

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
