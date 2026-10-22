// Package caller carries who is making a request, and the id that ties its log lines together,
// across the hop between the panel, the CLI, an agent's MCP call and the server.
//
// The identity travels in headers so it survives that hop, and on the context so a handler and
// the store beneath it read it without receiving it as an argument. It lets a log line say which
// agent asked, and lets komrad record that an agent read another agent's transcript
// deliberately. A request that names nobody is unattributed rather than wrong, so the
// logs show the panel and curl without the fields rather than with empty ones.
package caller

import (
	"context"
	"net/http"

	"komrad/lib/xhttp"

	"github.com/google/uuid"
)

const (
	TraceHeader   = "X-Request-Id"
	AgentHeader   = "X-Komrad-Agent-Id"
	SessionHeader = "X-Komrad-Session-Id"
)

type Caller struct {
	AgentID   uuid.UUID
	SessionID string
	Email     string
	Issuer    string
	Subject   string
}

// isKnown reports whether the request named an agent at all.
func (c Caller) isKnown() bool {
	return c.AgentID != uuid.Nil || c.SessionID != ""
}

// FromHeader reads the identity a request arrived with, dropping any value that is still an
// unexpanded shell variable or an agent id that does not parse.
func FromHeader(header http.Header) Caller {
	return Caller{
		AgentID:   parseAgentID(header.Get(AgentHeader)),
		SessionID: expanded(header.Get(SessionHeader)),
	}
}

func parseAgentID(value string) uuid.UUID {
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil
	}

	return id
}

func expanded(value string) string {
	if len(value) > 1 && value[0] == '$' {
		return ""
	}

	return value
}

// FromRequest reads the identity a request arrived with, keeping who the public listener found
// signed in on ctx.
func FromRequest(ctx context.Context, header http.Header) Caller {
	from := FromHeader(header)
	signedIn := From(ctx)
	from.Email, from.Issuer, from.Subject = signedIn.Email, signedIn.Issuer, signedIn.Subject

	return from
}

// SetHeader writes the identity onto a request going out, and nothing for a caller that is not
// known.
func (c Caller) SetHeader(header http.Header) {
	if c.AgentID != uuid.Nil {
		header.Set(AgentHeader, c.AgentID.String())
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
	if !from.isKnown() && trace == "" {
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
	from, ok := ctx.Value(callerKey{}).(Caller)
	if !ok {
		return Caller{}
	}

	return from
}

type traceKey struct{}

// WithTrace carries a request's trace id on a context.
func WithTrace(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceKey{}, id)
}

// Trace is the id that tags the log lines of the request being served, empty outside one.
func Trace(ctx context.Context) string {
	id, ok := ctx.Value(traceKey{}).(string)
	if !ok {
		return ""
	}

	return id
}

type renewalKey struct{}

// renewal takes a fresh ID token for the caller a request signed in as.
type renewal func(ctx context.Context, token string) error

// WithRenewal carries how a signed-in request takes a fresh token on a context.
func WithRenewal(ctx context.Context, renew renewal) context.Context {
	return context.WithValue(ctx, renewalKey{}, renew)
}

// Renew hands a fresh ID token to the request being served, and does nothing for a request that
// did not sign in.
func Renew(ctx context.Context, token string) error {
	renew, ok := ctx.Value(renewalKey{}).(renewal)
	if !ok {
		return nil
	}

	return renew(ctx, token)
}
