package interceptors

import (
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"

	"weave/internal/logging"
)

// NewLocalGuard wraps an HTTP handler so only a local caller reaches it. Nothing served
// here asks for a credential, since the service listens on loopback, so the request that
// was aimed somewhere else is refused before a handler sees it.
//
// It wraps the whole server rather than each route, so a route added later cannot forget
// it, which is the failure this exists to make impossible.
//
// Host is the check doing the work. It answers DNS rebinding, where a name is pointed at
// 127.0.0.1 after the fact: the request then looks same-origin by every rule, so Origin
// alone allows it, and the one thing it cannot claim is that it was aimed at loopback.
// Origin catches the plainer cross-site case and is what the MCP spec requires be refused.
//
// A request with no Origin passes. The panel, the health poll, the MCP tools calling back
// into this same server, and curl all arrive without one.
func NewLocalGuard(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reason := refusal(r)
		if reason == "" {
			next.ServeHTTP(w, r)

			return
		}

		// The refusal is logged here rather than left to the middleware around the MCP
		// endpoint, since this sits outside both that and the connect interceptors, so a
		// request stopped at this layer reaches nothing else that would record it.
		logger.LogAttrs(r.Context(), slog.LevelWarn, "refused a request that was not local",
			slog.String(logging.HTTPMethod, r.Method),
			slog.String(logging.URLPath, r.URL.Path),
			slog.String(logging.ClientAddress, r.RemoteAddr),
			slog.String(logging.RequestHost, r.Host),
			slog.String(logging.RequestOrigin, r.Header.Get(originHeader)),
			slog.Int(logging.HTTPStatus, http.StatusForbidden),
		)

		http.Error(w, "forbidden: "+reason, http.StatusForbidden)
	})
}

// originHeader names the page a request was sent by.
const originHeader = "Origin"

// refusal is why a request should be refused, or empty for one that should be served. The
// reason is answered to the caller, so it says which header was wrong without saying what
// would have been accepted.
func refusal(r *http.Request) string {
	// The Host check applies only to a connection that arrived on loopback. A server
	// deliberately bound to a routable address is reachable under its own name, and
	// refusing that name would break the thing somebody bound it for. The address is
	// read per connection rather than from the listener, so a server on 0.0.0.0 still
	// gets the check on everything reaching it through loopback.
	if servedOnLoopback(r) && !loopbackHost(r.Host) {
		return "the Host header is not a loopback address"
	}

	if origin := r.Header.Get(originHeader); origin != "" && !loopbackOrigin(origin) {
		return "the Origin header is not a loopback address"
	}

	return ""
}

// servedOnLoopback reports whether the connection the request arrived on was addressed to
// this machine. An address that cannot be read is treated as loopback, so the checks apply
// to a request whose provenance is unknown rather than being skipped for it.
func servedOnLoopback(r *http.Request) bool {
	local, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if !ok || local == nil {
		return true
	}

	return loopbackHost(local.String())
}

// loopbackHost reports whether a host, with or without a port, names this machine. The
// name is matched exactly rather than by suffix, since a host like localhost.example.com
// resolves to whatever its owner wants.
func loopbackHost(host string) bool {
	if hostname, _, err := net.SplitHostPort(host); err == nil {
		host = hostname
	}

	host = strings.Trim(host, "[]")

	if strings.EqualFold(host, "localhost") {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}

// loopbackOrigin reports whether an Origin header names this machine. Anything that is not
// a URL with a host is not, which is how the opaque "null" origin a sandboxed frame sends
// is refused rather than read as absent.
func loopbackOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}

	return loopbackHost(parsed.Host)
}
