package interceptors

import (
	"context"
	"errors"
	"net/http"

	"weave/internal/logging"

	"connectrpc.com/connect"
)

// errPanicked is what a caller is told when a handler panicked. The value itself is not passed
// on, since it can hold anything the code that raised it was holding.
var errPanicked = errors.New("the server panicked")

// NewRecovery answers a panicking RPC handler with an internal error. It is a handler option
// rather than an interceptor because connect recovers around the whole chain, so it covers the
// interceptors above the handler as well as the handler itself.
func NewRecovery() connect.HandlerOption {
	return connect.WithRecover(func(ctx context.Context, _ connect.Spec, _ http.Header, recovered any) error {
		return recoverRPC(ctx, recovered)
	})
}

// recoverRPC records a panicked call and says what the caller is told about it.
func recoverRPC(ctx context.Context, recovered any) error {
	logging.LogPanic(ctx, recovered)

	return connect.NewError(connect.CodeInternal, errPanicked)
}

// serveAndRecover runs the handler and answers a panic with a 500. The MCP endpoint is served
// as plain HTTP, which connect's own recovery never sees, so this is what stands in for it.
//
// The status is written rather than left alone so the client is told, and so the line its
// caller logs for the request says how the request ended.
func serveAndRecover(recorder *statusRecorder, r *http.Request, next http.Handler) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}

		// net/http's own signal for a handler dropping a request on purpose. It is not a
		// failure, and the server above is what it is meant for, so it is passed along rather
		// than logged and answered.
		if err, ok := recovered.(error); ok && errors.Is(err, http.ErrAbortHandler) {
			panic(recovered)
		}

		logging.LogPanic(r.Context(), recovered)

		// A handler that panicked partway through a response has already sent its status, and
		// a second one is a lie the client cannot read anyway.
		if !recorder.written {
			recorder.WriteHeader(http.StatusInternalServerError)
		}
	}()

	next.ServeHTTP(recorder, r)
}
