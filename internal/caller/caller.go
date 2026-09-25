// Package caller carries who is making a request, and the id that ties its log lines together,
// across the hop between the panel, the CLI, an agent's MCP call and the service.
//
// The identity travels in headers so it survives that hop, and on the context so a handler and
// the store beneath it read it without being handed it. It is what lets a log line say which
// agent asked, and what lets an agent reading another agent's transcript be recorded as having
// done so deliberately. A request that names nobody is unattributed rather than wrong, so the
// panel and curl are logged without the fields rather than with empty ones.
package caller

import (
	"context"
	"net/http"

	"komrad/lib/xhttp"
)

const (
	TraceHeader   = "X-Request-Id"
	AgentHeader   = "X-Komrad-Agent-Id"
	SessionHeader = "X-Komrad-Session-Id"
)

type Caller struct {
	AgentID   string
	SessionID string
}

// IsKnown reports whether the request named an agent at all.
func (c Caller) IsKnown() bool {
	return c.AgentID != "" || c.SessionID != ""
}

// FromHeader reads the identity a request arrived with, dropping any value that is still an
// unexpanded shell variable.
func FromHeader(header http.Header) Caller {
	return Caller{
		AgentID:   expanded(header.Get(AgentHeader)),
		SessionID: expanded(header.Get(SessionHeader)),
	}
}

func expanded(value string) string {
	if len(value) > 1 && value[0] == '$' {
		return ""
	}

	return value
}

// SetHeader writes the identity onto a request going out, and nothing for a caller that is not
// known.
func (c Caller) SetHeader(header http.Header) {
	if c.AgentID != "" {
		header.Set(AgentHeader, c.AgentID)
	}
	if c.SessionID != "" {
		header.Set(SessionHeader, c.SessionID)
	}
}

// Transport stamps every request a client sends with the identity and trace of the request being
// served, taking next as the transport underneath or xhttp's when nil.
func Transport(next http.RoundTripper) http.RoundTripper {
	if next == nil {
		next = xhttp.NewTransport()
	}

	return forwarding{next: next}
}

type forwarding struct{ next http.RoundTripper }

// RoundTrip copies what the context carries onto a clone of the request.
func (f forwarding) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()

	from, trace := From(ctx), Trace(ctx)
	if !from.IsKnown() && trace == "" {
		return f.next.RoundTrip(req)
	}

	forwarded := req.Clone(ctx)
	from.SetHeader(forwarded.Header)

	if trace != "" {
		forwarded.Header.Set(TraceHeader, trace)
	}

	return f.next.RoundTrip(forwarded)
}

type callerKey struct{}

// With carries the identity on a context, so a handler and anything it calls can say who asked.
func With(ctx context.Context, from Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, from)
}

// From is the identity carried on a context, zero outside a request or on one that arrived
// without it.
func From(ctx context.Context) Caller {
	from, _ := ctx.Value(callerKey{}).(Caller)

	return from
}

type traceKey struct{}

// WithTrace carries a request's trace id on a context.
func WithTrace(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceKey{}, id)
}

// Trace is the id the request being served is logged under, empty outside one.
func Trace(ctx context.Context) string {
	id, _ := ctx.Value(traceKey{}).(string)

	return id
}
