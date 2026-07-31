package interceptors

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"weave/internal/logging"

	"github.com/google/uuid"
)

// mcpBodyPeek bounds how much of a request is read to find what it is calling. An MCP
// call is a small JSON-RPC envelope and the tool arguments are the only part that varies,
// so anything past this is not a call whose name is worth buffering the body to learn.
const mcpBodyPeek = 64 << 10

// NewLoggingHandler wraps an HTTP handler so every request through it is recorded, which
// is how the MCP endpoint reaches the log. It is served as plain HTTP on the same mux as
// the RPCs, so the connect interceptor beside this never sees it, and without this it is
// the one surface a model can list, spawn, and delete agents through leaving no trace.
//
// Calls are written at info rather than at the debug the served RPCs get. Nothing polls
// this, so a line here is a person or a model asking for something, and there are a
// handful an hour rather than four a second.
func NewLoggingHandler(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		trace := r.Header.Get(TraceHeader)
		if trace == "" {
			trace = uuid.NewString()
		}

		method, tool := mcpCall(r)

		attrs := []slog.Attr{
			slog.String(logging.TraceID, trace),
			slog.String(logging.HTTPMethod, r.Method),
			slog.String(logging.URLPath, r.URL.Path),
			slog.String(logging.ClientAddress, r.RemoteAddr),
		}
		if method != "" {
			attrs = append(attrs, slog.String(logging.RPCMethod, method))
		}
		if tool != "" {
			attrs = append(attrs, slog.String(logging.MCPTool, tool))
		}

		ctx := logging.WithTraceID(logging.WithLogger(r.Context(), logger.With(logging.Attrs(attrs)...)), trace)

		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		recorder.Header().Set(TraceHeader, trace)

		start := time.Now()
		next.ServeHTTP(recorder, r.WithContext(ctx))

		attrs = append(attrs,
			slog.Int(logging.HTTPStatus, recorder.status),
			slog.Int64(logging.DurationMS, time.Since(start).Milliseconds()),
		)

		// The status is the only thing that says whether it worked. Nothing below returns an
		// error to this layer, so a failure is a 4xx or a 5xx and is logged as one rather
		// than sitting at info among the calls that were served.
		level := slog.LevelInfo
		if recorder.status >= http.StatusBadRequest {
			level = slog.LevelError
		}

		logger.LogAttrs(ctx, level, "http served", attrs...)
	})
}

// mcpCall is the JSON-RPC method and, for a tool call, the tool it names. The body is
// consumed to find them and then put back for the handler, since the request is a stream
// that can only be read once and the handler below still needs all of it.
//
// Anything that does not parse gives nothing rather than an error. A body that is not the
// envelope this looks for is a request this does not know how to describe, and a line
// naming only the path is still worth writing.
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

// peekedBody is the request body with what was read to identify the call put back in front
// of it. Closing still closes the original, which is the handler's to release.
type peekedBody struct {
	io.Reader
	closer io.Closer
}

func (b *peekedBody) Close() error { return b.closer.Close() }

// statusRecorder remembers the status a handler wrote, which is the only thing at this
// layer that says whether the call worked.
//
// Unwrap keeps the wrapper from breaking the handler under it. MCP streams its responses,
// and a http.ResponseController asked to flush finds the real writer through this rather
// than failing because a wrapper is in the way.
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
