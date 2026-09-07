package interceptors

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"weave/internal/caller"
	"weave/internal/logging"

	"connectrpc.com/connect"
	"github.com/google/uuid"
)

const TraceHeader = "X-Request-Id"

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

		from := caller.FromHeader(req.Header())
		attrs = append(attrs, callerAttrs(from)...)

		logger := l.logger.With(logging.Attrs(attrs)...)
		ctx = caller.With(caller.WithTrace(logging.WithLogger(ctx, logger), trace), from)

		start := time.Now()
		res, err := next(ctx, req)
		elapsed := slog.Int64(logging.DurationMS, time.Since(start).Milliseconds())

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

// WrapStreamingClient and WrapStreamingHandler pass through, since the service is unary only.
func (l *loggingInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (l *loggingInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

// callerAttrs name the agent a call came from, and nothing at all for the panel or a person,
// so one agent's calls are one filter however they arrived.
func callerAttrs(from caller.Caller) []slog.Attr {
	attrs := make([]slog.Attr, 0, 2)
	if from.AgentID != "" {
		attrs = append(attrs, slog.String(logging.AgentID, from.AgentID))
	}
	if from.SessionID != "" {
		attrs = append(attrs, slog.String(logging.SessionID, from.SessionID))
	}

	return attrs
}

func failure(err error, elapsed slog.Attr) []slog.Attr {
	attrs := []slog.Attr{elapsed, slog.String(logging.RPCErrorCode, code(err))}

	return append(attrs, logging.Err(err)...)
}

func code(err error) string {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Code().String()
	}

	return connect.CodeUnknown.String()
}

func withTrace(err error, trace string) error {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		connectErr.Meta().Set(TraceHeader, trace)
	}

	return err
}

func splitProcedure(procedure string) (string, string) {
	trimmed := strings.TrimPrefix(procedure, "/")

	service, method, found := strings.Cut(trimmed, "/")
	if !found {
		return "", trimmed
	}

	return service, method
}
