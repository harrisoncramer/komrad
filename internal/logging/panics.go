package logging

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
)

// What a panic looks like in the log. Go's own account of one is plain text on stderr with no
// fields to filter on, and in the service it lands in the middle of a file of records. So
// every place a panic surfaces writes one of these first, with the stack on the record rather
// than on the lines around it.

// LogPanic writes one record for a panic, with the stack it was recovered from. It must be
// called from the recovery itself, since the frames that panicked are on the stack while the
// deferred function runs and gone once it has returned.
func LogPanic(ctx context.Context, recovered any) {
	From(ctx).LogAttrs(ctx, slog.LevelError, "panicked",
		slog.String(ErrorType, fmt.Sprintf("%T", recovered)),
		slog.String(ErrorMessage, fmt.Sprint(recovered)),
		slog.String(StackTrace, string(debug.Stack())))
}

// LogAndRepanic records a panic and then lets it carry on. It is deferred at the head of every
// root one can reach, meaning the process itself and each goroutine, since Go ends the program
// on a panic nothing catches and a recovery deferred in one goroutine is never reached from
// another.
//
// The panic is raised again rather than swallowed, so the exit code and Go's own account on
// stderr stay what they were. All this adds is the record, written while there is still a
// logger to write it with.
func LogAndRepanic(ctx context.Context) {
	recovered := recover()
	if recovered == nil {
		return
	}

	LogPanic(ctx, recovered)

	panic(recovered)
}
