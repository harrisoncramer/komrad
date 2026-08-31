package caller

import (
	"context"
	"net/http"
)

const (
	TraceHeader = "X-Request-Id"

	AgentHeader = "X-Weave-Agent-Id"

	SessionHeader = "X-Weave-Session-Id"

	SetupRanHeader = "X-Weave-Setup-Ran"
)

type Caller struct {
	AgentID   string
	SessionID string
}

// IsKnown reports whether the request named an agent at all; a call from the panel or curl
// is unattributed rather than wrong, and is logged without the fields rather than empty ones.
func (c Caller) IsKnown() bool {
	return c.AgentID != "" || c.SessionID != ""
}

// FromHeader reads the identity a request arrived with, dropping any value that is still a
// shell variable, since an unexpanded one means the CLI's environment never set it.
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

// SetHeader writes the identity onto a request going out, and nothing at all for a caller
// that is not known, since an empty header reads as an answer.
func (c Caller) SetHeader(header http.Header) {
	if c.AgentID != "" {
		header.Set(AgentHeader, c.AgentID)
	}
	if c.SessionID != "" {
		header.Set(SessionHeader, c.SessionID)
	}
}

// Transport stamps every request a client sends with the identity and trace of the request
// being served, so a call made on somebody's behalf stays attributed to them; next is the
// transport underneath, or the default one when nil.
func Transport(next http.RoundTripper) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}

	return forwarding{next: next}
}

type forwarding struct{ next http.RoundTripper }

// RoundTrip copies what the context carries onto a clone of the request, since the request
// handed to a transport belongs to whoever built it and must not be modified.
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

// With carries the identity on a context, which is how a handler and anything it calls can
// say who asked.
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
