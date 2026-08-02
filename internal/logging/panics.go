package logging

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
)

// LogPanic writes one record for a panic, with the stack it was recovered from. It must be
// called from the recovery itself, since the frames that panicked are on the stack while the
// deferred function runs and gone once it has returned.
func LogPanic(ctx context.Context, recovered any) {
	From(ctx).LogAttrs(ctx, slog.LevelError, "panicked",
		slog.String(ErrorType, fmt.Sprintf("%T", recovered)),
		slog.String(ErrorMessage, fmt.Sprint(recovered)),
		slog.String(StackTrace, string(debug.Stack())))
}

// LogAndRepanic records a panic and then lets it carry on.
func LogAndRepanic(ctx context.Context) {
	recovered := recover()
	if recovered == nil {
		return
	}

	LogPanic(ctx, recovered)

	panic(recovered)
}
