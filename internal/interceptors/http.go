// Package interceptors holds the rules every request passes through before a handler sees it:
// the log line it is recorded in, the panic that would otherwise take the service down, the
// protovalidate constraints declared in the proto, and the guard that refuses a call aimed at
// this machine from somewhere else.
package interceptors

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"weave/internal/caller"
	"weave/internal/logging"

	"github.com/google/uuid"
)

const mcpBodyPeek = 64 << 10

// NewLoggingHandler records every request to the MCP endpoint.
func NewLoggingHandler(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		trace := r.Header.Get(TraceHeader)
		if trace == "" {
			trace = uuid.NewString()
		}

		method, tool := mcpCall(r)

		from := caller.FromHeader(r.Header)

		attrs := []slog.Attr{
			slog.String(logging.TraceID, trace),
			slog.String(logging.HTTPMethod, r.Method),
			slog.String(logging.URLPath, r.URL.Path),
			slog.String(logging.ClientAddress, r.RemoteAddr),
		}
		attrs = append(attrs, callerAttrs(from)...)
		if method != "" {
			attrs = append(attrs, slog.String(logging.RPCMethod, method))
		}
		if tool != "" {
			attrs = append(attrs, slog.String(logging.MCPTool, tool))
		}

		ctx := logging.WithLogger(r.Context(), logger.With(logging.Attrs(attrs)...))
		ctx = caller.With(caller.WithTrace(ctx, trace), from)

		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		recorder.Header().Set(TraceHeader, trace)

		start := time.Now()
		serveAndRecover(recorder, r.WithContext(ctx), next)

		attrs = append(attrs,
			slog.Int(logging.HTTPStatus, recorder.status),
			slog.Int64(logging.DurationMS, time.Since(start).Milliseconds()),
		)

		// Info rather than the debug a served RPC gets, nothing polling this endpoint.
		level := slog.LevelInfo
		switch {
		case recorder.status >= http.StatusInternalServerError:
			level = slog.LevelError
		case recorder.status >= http.StatusBadRequest:
			level = slog.LevelWarn
		}

		logger.LogAttrs(ctx, level, "http served", attrs...)
	})
}

func mcpCall(r *http.Request) (string, string) {
	if r.Body == nil {
		return "", ""
	}

	peeked, err := io.ReadAll(io.LimitReader(r.Body, mcpBodyPeek))
	if err != nil {
		return "", ""
	}

	r.Body = &peekedBody{Reader: io.MultiReader(bytes.NewReader(peeked), r.Body), closer: r.Body}

	var envelope struct {
		Method string `json:"method"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if err := json.Unmarshal(peeked, &envelope); err != nil {
		return "", ""
	}

	return envelope.Method, envelope.Params.Name
}

type peekedBody struct {
	io.Reader
	closer io.Closer
}

func (b *peekedBody) Close() error { return b.closer.Close() }

type statusRecorder struct {
	http.ResponseWriter
	status  int
	written bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if !r.written {
		r.status, r.written = status, true
	}

	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.written = true

	return r.ResponseWriter.Write(b)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
