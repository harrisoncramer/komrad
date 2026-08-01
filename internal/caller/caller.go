// Package caller is who a request is from. What reaches the service is either the panel,
// which is the person, or an agent the panel spawned calling back through its tools, and a
// log that cannot tell one agent from another cannot answer which of them deleted
// something.
//
// The identity travels as headers because that is the only channel there is: an agent runs
// somebody else's CLI, and all the panel decides is the server registration it is launched
// with, which every CLI here can be told to put a header on.
package caller

import (
	"context"
	"net/http"
)

// The headers an identity travels in. The trace id sits with them rather than in the
// middleware that reads it, since anything forwarding one of these forwards all of them.
const (
	// TraceHeader carries the id one request is logged under. A caller that already has
	// one sends it, which is what makes a call that crossed two processes one search.
	TraceHeader = "X-Request-Id"
	// AgentHeader is the panel's own id for the agent making the call.
	AgentHeader = "X-Weave-Agent-Id"
	// SessionHeader is the CLI conversation that agent is running, which is what ties a
	// call to the transcript the panel holds of what was being said around it.
	SessionHeader = "X-Weave-Session-Id"
)

// Caller is the agent a request came from, zero for anything else. The panel sends none of
// this: it is the user, and there is only one of them.
type Caller struct {
	AgentID   string
	SessionID string
}

// Known reports whether the request named an agent at all. A call from the panel or from a
// person with curl is unattributed rather than wrong, and is logged without the fields
// rather than with empty ones.
func (c Caller) Known() bool {
	return c.AgentID != "" || c.SessionID != ""
}

// FromHeader reads the identity a request arrived with. A value that is still a shell
// variable is dropped, since the CLIs fill these in from the agent's environment and an
// unexpanded one means the variable was never set.
func FromHeader(header http.Header) Caller {
	return Caller{
		AgentID:   expanded(header.Get(AgentHeader)),
		SessionID: expanded(header.Get(SessionHeader)),
	}
}

// expanded is the value, or empty if the CLI passed the variable through untouched.
func expanded(value string) string {
	if len(value) > 1 && value[0] == '$' {
		return ""
	}

	return value
}

// SetHeader writes the identity onto a request going out. Nothing is written for a caller
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
// being served, so a call the service makes on somebody's behalf stays attributed to them.
// next is the transport underneath, or the default one when nil.
//
// It is a transport rather than something each caller remembers to do, since the client
// that forgets is the one whose calls read as the service talking to itself.
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
	if !from.Known() && trace == "" {
		return f.next.RoundTrip(req)
	}

	forwarded := req.Clone(ctx)
	from.SetHeader(forwarded.Header)

	if trace != "" {
		forwarded.Header.Set(TraceHeader, trace)
	}

	return f.next.RoundTrip(forwarded)
}

// callerKey is what the identity is held under on a context. It is an unexported type so
// nothing outside this package can collide with it.
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

// traceKey is what the request's trace id is held under, separately from the identity,
// since a request has one whether or not it says who it is from.
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
