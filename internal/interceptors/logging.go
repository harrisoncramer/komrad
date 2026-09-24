package interceptors

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"komrade/internal/caller"
	"komrade/internal/logging"

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
		trace, logger, ctx := l.open(ctx, req.Spec(), req.Peer(), req.Header())

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

// WrapStreamingClient passes through on a service that opens no streams of its own.
func (l *loggingInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

// WrapStreamingHandler opens a trace that lasts the stream's life and logs its close with how
// long it lived and how many messages it sent.
func (l *loggingInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		trace, logger, ctx := l.open(ctx, conn.Spec(), conn.Peer(), conn.RequestHeader())
		conn.ResponseHeader().Set(TraceHeader, trace)

		counted := &countingConn{StreamingHandlerConn: conn}
		start := time.Now()
		err := next(ctx, counted)
		elapsed := slog.Int64(logging.DurationMS, time.Since(start).Milliseconds())
		sent := slog.Int64(logging.RPCMessagesSent, counted.sent.Load())

		if err != nil {
			logger.LogAttrs(ctx, slog.LevelError, "stream failed", append(failure(err, elapsed), sent)...)

			return withTrace(err, trace)
		}

		logger.LogAttrs(ctx, slog.LevelDebug, "stream closed", elapsed, sent)

		return nil
	}
}

// open reads the trace a caller sent or mints one, and hands back the logger and context a
// call runs under.
func (l *loggingInterceptor) open(ctx context.Context, spec connect.Spec, peer connect.Peer, header http.Header) (string, *slog.Logger, context.Context) {
	trace := header.Get(TraceHeader)
	if trace == "" {
		trace = uuid.NewString()
	}

	service, method := splitProcedure(spec.Procedure)
	attrs := []slog.Attr{
		slog.String(logging.TraceID, trace),
		slog.String(logging.RPCSystem, logging.RPCSystemConnect),
		slog.String(logging.RPCService, service),
		slog.String(logging.RPCMethod, method),
		slog.String(logging.ClientAddress, peer.Addr),
	}

	from := caller.FromHeader(header)
	attrs = append(attrs, callerAttrs(from)...)

	logger := l.logger.With(logging.Attrs(attrs)...)

	return trace, logger, caller.With(caller.WithTrace(logging.WithLogger(ctx, logger), trace), from)
}

// countingConn counts the messages a handler sends down a stream.
type countingConn struct {
	connect.StreamingHandlerConn
	sent atomic.Int64
}

func (c *countingConn) Send(msg any) error {
	if err := c.StreamingHandlerConn.Send(msg); err != nil {
		return err
	}

	c.sent.Add(1)

	return nil
}

// callerAttrs name the agent a call came from, and nothing at all for the panel or a person.
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
