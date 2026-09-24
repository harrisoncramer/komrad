package interceptors

import (
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"

	"komrade/internal/logging"
)

// NewLocalGuard refuses a request that was aimed somewhere else before a handler sees it, which
// is what stops a DNS rebinding attack reaching the ConnectRPC endpoint.
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

const originHeader = "Origin"

func refusal(r *http.Request) string {
	if isServedOnLoopback(r) && !isLoopbackHost(r.Host) {
		return "the Host header is not a loopback address"
	}

	if origin := r.Header.Get(originHeader); origin != "" && !isLoopbackOrigin(origin) {
		return "the Origin header is not a loopback address"
	}

	return ""
}

func isServedOnLoopback(r *http.Request) bool {
	local, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if !ok || local == nil {
		return true
	}

	return isLoopbackHost(local.String())
}

func isLoopbackHost(host string) bool {
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

func isLoopbackOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}

	return isLoopbackHost(parsed.Host)
}
