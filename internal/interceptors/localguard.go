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
// This is to prevent DNS rebinding attacks, where an external source could arbitrarily
// call to the ConnectRPC endpoint.
func NewLocalGuard(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reason := refusal(r)
		if reason == "" {
			next.ServeHTTP(w, r)

			return
		}

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

// refusal is why a request should be refused, or empty for one that should be served.
func refusal(r *http.Request) string {
	if servedOnLoopback(r) && !loopbackHost(r.Host) {
		return "the Host header is not a loopback address"
	}

	if origin := r.Header.Get(originHeader); origin != "" && !loopbackOrigin(origin) {
		return "the Origin header is not a loopback address"
	}

	return ""
}

// servedOnLoopback reports whether the connection the request arrived on was addressed to
// this machine.
func servedOnLoopback(r *http.Request) bool {
	local, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if !ok || local == nil {
		return true
	}

	return loopbackHost(local.String())
}

// loopbackHost reports whether a host, with or without a port, names this machine.
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

// loopbackOrigin reports whether an Origin header names this machine.
func loopbackOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}

	return loopbackHost(parsed.Host)
}
