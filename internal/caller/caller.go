// Package caller is who a request is from.
//
// Most of what reaches the service is the panel, which is the person sitting in front of
// it. The rest is agents the panel spawned, calling back through the tools they were
// launched with. Those two are worth telling apart in the log, and one agent is worth
// telling from another: an agent that deleted something, or that hammered a tool in a
// loop, is a question about which agent, and a record saying only that a call arrived from
// loopback cannot answer it.
//
// The identity travels as headers because that is the only channel there is. An agent runs
// somebody else's CLI in a pane, and the one thing the panel decides about how it calls
// back is the server registration it is launched with, which every CLI here can be told to
// put a header on.
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

// Known reports whether the request named an agent at all. A call from the panel, from a
// person with curl, or from an agent launched before the panel started sending this is
// unattributed rather than wrong, and is logged without the fields rather than with empty
// ones.
func (c Caller) Known() bool {
	return c.AgentID != "" || c.SessionID != ""
}

// FromHeader reads the identity a request arrived with. A value that is still a shell
// variable is dropped: the CLIs are told to fill these in from the agent's environment,
// and one that arrives unexpanded means the variable was not set, which is no identity
// rather than an agent called ${WEAVE_AGENT_ID}.
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

// SetHeader writes the identity onto a request going out, so a call the service makes on a
// caller's behalf arrives attributed to them rather than to the service. Nothing is written
// for a caller that is not known, since an empty header reads as an answer.
func (c Caller) SetHeader(header http.Header) {
	if c.AgentID != "" {
		header.Set(AgentHeader, c.AgentID)
	}
	if c.SessionID != "" {
		header.Set(SessionHeader, c.SessionID)
	}
}

// Transport stamps every request a client sends with the identity and trace of the request
// being served, and is how a call the service makes on somebody's behalf stays attributed
// to them. next is the transport underneath, or the default one when nil.
//
// It is a transport rather than something each caller remembers to do, because the
// alternative is every outbound client copying the same headers, and the one that forgets
// is the one whose calls read in the log as the service talking to itself.
func Transport(next http.RoundTripper) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}

	return forwarding{next: next}
}

type forwarding struct{ next http.RoundTripper }

// RoundTrip copies what the context carries onto a clone of the request. The request handed
// to a transport belongs to whoever built it and must not be modified, which is why this
// clones before touching the headers.
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

// With carries the identity on a context, which is how the handler under a request, and
// anything it calls in turn, can say who asked.
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
