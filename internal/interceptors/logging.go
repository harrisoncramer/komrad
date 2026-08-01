package interceptors

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"weave/internal/caller"
	"weave/internal/logging"

	"connectrpc.com/connect"
	"github.com/google/uuid"
)

// TraceHeader is where a request's trace id is read from and written back to. A caller
// that already has one sends it and gets its own id back on the response, so a failure
// someone is looking at is found in the log by the id they were shown rather than by the
// time it happened.
const TraceHeader = "X-Request-Id"

// loggingInterceptor writes one record per call and hands every handler below it a
// logger already carrying that call's attributes. It is the only place a request is
// logged, so the panel polling four times a second and a model calling a tool through
// MCP produce the same shaped line.
type loggingInterceptor struct {
	logger *slog.Logger
}

// NewLogging builds the interceptor over the service's logger.
func NewLogging(logger *slog.Logger) connect.Interceptor {
	return &loggingInterceptor{logger: logger}
}

func (l *loggingInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		trace := req.Header().Get(TraceHeader)
		if trace == "" {
			trace = uuid.NewString()
		}

		service, method := splitProcedure(req.Spec().Procedure)
		attrs := []slog.Attr{
			slog.String(logging.TraceID, trace),
			slog.String(logging.RPCSystem, logging.RPCSystemConnect),
			slog.String(logging.RPCService, service),
			slog.String(logging.RPCMethod, method),
			slog.String(logging.ClientAddress, req.Peer().Addr),
		}

		// The panel and every agent it spawned reach this over the same loopback
		// connection, which the address tells apart not at all, so a call says which agent
		// it is from or is left unattributed.
		from := caller.FromHeader(http.Header(req.Header()))
		attrs = append(attrs, CallerAttrs(from)...)

		logger := l.logger.With(logging.Attrs(attrs)...)
		ctx = caller.With(caller.WithTrace(logging.WithLogger(ctx, logger), trace), from)

		start := time.Now()
		res, err := next(ctx, req)
		elapsed := slog.Int64(logging.DurationMS, time.Since(start).Milliseconds())

		// A call that failed is logged whole, since the log is read to find out why
		// something broke. A call that worked is logged at debug. The panel lists
		// everything it draws every second it is open, so an info line per call would
		// bury every failure under a thousand successes an hour.
		if err != nil {
			logger.LogAttrs(ctx, slog.LevelError, "rpc failed", failure(err, elapsed)...)

			return res, withTrace(err, trace)
		}

		logger.LogAttrs(ctx, slog.LevelDebug, "rpc served", elapsed)

		if res != nil {
			res.Header().Set(TraceHeader, trace)
		}

		return res, nil
	}
}

// WrapStreamingClient and WrapStreamingHandler pass through, as the validation
// interceptor's do. The service is unary only.
func (l *loggingInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (l *loggingInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

// CallerAttrs name the agent a call came from, and nothing at all for a call from the
// panel or from a person, which is unattributed rather than anonymous. Both edges of the
// service log it the same way, so one agent's calls are one filter however they arrived.
func CallerAttrs(from caller.Caller) []slog.Attr {
	attrs := make([]slog.Attr, 0, 2)
	if from.AgentID != "" {
		attrs = append(attrs, slog.String(logging.AgentID, from.AgentID))
	}
	if from.SessionID != "" {
		attrs = append(attrs, slog.String(logging.SessionID, from.SessionID))
	}

	return attrs
}

// failure is what a failed call adds to its record. The connect code is pulled out
// because it separates a caller sending something invalid from the service itself
// breaking, and a log where both read as "rpc failed" cannot be triaged.
func failure(err error, elapsed slog.Attr) []slog.Attr {
	attrs := []slog.Attr{elapsed, slog.String(logging.RPCErrorCode, code(err))}

	return append(attrs, logging.Err(err)...)
}

// code is the connect code of a failed call. An error that is not a connect error is one
// a handler returned bare, which connect answers as unknown, so that is what is recorded
// rather than an empty field.
func code(err error) string {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Code().String()
	}

	return connect.CodeUnknown.String()
}

// withTrace puts the trace id on a failure's metadata, which is the only way it reaches
// the caller, since connect sends no headers of its own for an error. A bare error is
// left alone rather than wrapped, because turning it into a connect error here would
// change the code the caller sees.
func withTrace(err error, trace string) error {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		connectErr.Meta().Set(TraceHeader, trace)
	}

	return err
}

// splitProcedure breaks connect's procedure path into the service and the method, which
// are separate fields so a log can be read by service. A path in any other shape is kept
// whole as the method, since a record naming the wrong thing is worse than one naming
// less.
func splitProcedure(procedure string) (string, string) {
	trimmed := strings.TrimPrefix(procedure, "/")

	service, method, found := strings.Cut(trimmed, "/")
	if !found {
		return "", trimmed
	}

	return service, method
}
