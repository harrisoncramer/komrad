package interceptors

import (
	"context"
	"errors"
	"net/http"

	"weave/internal/logging"

	"connectrpc.com/connect"
)

var errPanicked = errors.New("the server panicked")

// NewRecovery answers a panicking RPC handler with an internal error. It is a handler option
// rather than an interceptor because connect recovers around the whole chain, so it covers the
// interceptors above the handler as well as the handler itself.
func NewRecovery() connect.HandlerOption {
	return connect.WithRecover(func(ctx context.Context, _ connect.Spec, _ http.Header, recovered any) error {
		return recoverRPC(ctx, recovered)
	})
}

func recoverRPC(ctx context.Context, recovered any) error {
	logging.LogPanic(ctx, recovered)

	return connect.NewError(connect.CodeInternal, errPanicked)
}

func serveAndRecover(recorder *statusRecorder, r *http.Request, next http.Handler) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}

		if err, ok := recovered.(error); ok && errors.Is(err, http.ErrAbortHandler) {
			panic(recovered)
		}

		logging.LogPanic(r.Context(), recovered)

		if !recorder.written {
			recorder.WriteHeader(http.StatusInternalServerError)
		}
	}()

	next.ServeHTTP(recorder, r)
}
