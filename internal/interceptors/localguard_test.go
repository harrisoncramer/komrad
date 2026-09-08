package interceptors

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"weave/internal/logging"
	"weave/internal/logtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLocalGuard pins which requests reach the service and which are refused, so a page in a
// browser cannot drive the local API.
func TestLocalGuard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		host      string
		origin    string
		fetchSite string
		localAddr string
		want      int
	}{
		{
			name:      "a foreign origin is refused",
			host:      "127.0.0.1:8787",
			origin:    "https://evil.example.com",
			fetchSite: "cross-site",
			localAddr: "127.0.0.1:8787",
			want:      http.StatusForbidden,
		},
		{
			name:      "a rebound name is refused on its host though it is same-origin",
			host:      "evil.example.com:8787",
			origin:    "http://evil.example.com:8787",
			fetchSite: "same-origin",
			localAddr: "127.0.0.1:8787",
			want:      http.StatusForbidden,
		},
		{
			name:      "a host under localhost belongs to whoever owns the name",
			host:      "localhost.evil.example.com:8787",
			localAddr: "127.0.0.1:8787",
			want:      http.StatusForbidden,
		},
		{
			name:      "an opaque origin is refused rather than read as absent",
			host:      "127.0.0.1:8787",
			origin:    "null",
			localAddr: "127.0.0.1:8787",
			want:      http.StatusForbidden,
		},
		{
			name:      "a host that cannot be read is checked rather than skipped",
			host:      "evil.example.com:8787",
			localAddr: "",
			want:      http.StatusForbidden,
		},
		{
			name:      "the panel sends no origin",
			host:      "127.0.0.1:8787",
			localAddr: "127.0.0.1:8787",
			want:      http.StatusOK,
		},
		{
			name:      "a loopback origin is served",
			host:      "127.0.0.1:8787",
			origin:    "http://127.0.0.1:8787",
			localAddr: "127.0.0.1:8787",
			want:      http.StatusOK,
		},
		{
			name:      "localhost is a loopback name",
			host:      "localhost:8787",
			origin:    "http://localhost:8787",
			localAddr: "127.0.0.1:8787",
			want:      http.StatusOK,
		},
		{
			name:      "ipv6 loopback is served",
			host:      "[::1]:8787",
			origin:    "http://[::1]:8787",
			localAddr: "[::1]:8787",
			want:      http.StatusOK,
		},
		{
			name:      "a server bound to a routable address answers under its own name",
			host:      "workstation.example.com:8787",
			localAddr: "192.168.1.20:8787",
			want:      http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, served, written := guardedCall(t,
				createAgentCall(tt.host, tt.origin, tt.fetchSite), tt.localAddr)

			assert.Equal(t, tt.want, status)
			assert.Equal(t, tt.want == http.StatusOK, served,
				"a refused request must not reach the handler")

			if tt.want == http.StatusOK {
				assert.Empty(t, written, "a served request is left to the layers below")

				return
			}

			record := logtest.Record(t, written)
			assert.Equal(t, "WARN", record["severity_text"])
			assert.Equal(t, tt.host, record[logging.RequestHost])
			assert.EqualValues(t, http.StatusForbidden, record[logging.HTTPStatus])
		})
	}
}

// TestLocalGuardOverRealConnection serves the guard on a socket, since the Host check rests
// on net/http setting an address the rows above set for themselves.
func TestLocalGuardOverRealConnection(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(NewLocalGuard(logging.New(&bytes.Buffer{}, slog.LevelInfo),
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })))
	defer server.Close()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, nil)
	require.NoError(t, err)

	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	assert.Equal(t, http.StatusOK, res.StatusCode, "a call to the address it is listening on")

	rebound, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, nil)
	require.NoError(t, err)
	rebound.Host = "evil.example.com"

	res, err = http.DefaultClient.Do(rebound)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	assert.Equal(t, http.StatusForbidden, res.StatusCode, "the same socket, aimed at another name")
}

// TestLocalGuardRefusesEveryMethod covers the methods a CSRF check would let through as safe,
// since connect serves idempotent unary RPCs over GET when a client asks.
func TestLocalGuardRefusesEveryMethod(t *testing.T) {
	t.Parallel()

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(method, "/agent.v1.AgentService/ListAgents", nil)
			req.Host = "127.0.0.1:8787"
			req.Header.Set("Origin", "https://evil.example.com")

			status, served, _ := guardedCall(t, req, "127.0.0.1:8787")

			assert.Equal(t, http.StatusForbidden, status)
			assert.False(t, served)
		})
	}
}

func guardedCall(t *testing.T, req *http.Request, localAddr string) (int, bool, string) {
	t.Helper()

	if localAddr != "" {
		addr, err := net.ResolveTCPAddr("tcp", localAddr)
		require.NoError(t, err)

		req = req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey, net.Addr(addr)))
	}

	var out bytes.Buffer

	served := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		served = true
		w.WriteHeader(http.StatusOK)
	})

	recorder := httptest.NewRecorder()
	NewLocalGuard(logging.New(&out, slog.LevelInfo), next).ServeHTTP(recorder, req)

	return recorder.Code, served, out.String()
}

func createAgentCall(host, origin, fetchSite string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/agent.v1.AgentService/CreateAgent",
		strings.NewReader(`{"name":"x"}`))
	req.Host = host
	req.Header.Set("Content-Type", "application/json")

	if origin != "" {
		req.Header.Set("Origin", origin)
	}

	if fetchSite != "" {
		req.Header.Set("Sec-Fetch-Site", fetchSite)
	}

	return req
}
