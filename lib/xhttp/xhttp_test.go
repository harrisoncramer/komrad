package xhttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestJSON covers the one call: headers and body go out as given, a 2xx is decoded, and a
// failure comes back with its status and body rather than as an error.
func TestJSON(t *testing.T) {
	t.Parallel()

	type echo struct {
		Method string          `json:"method"`
		Auth   string          `json:"auth"`
		Accept string          `json:"accept"`
		Type   string          `json:"type"`
		Body   json.RawMessage `json:"body"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/refuse" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))

			return
		}

		var body json.RawMessage
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		_ = json.NewEncoder(w).Encode(echo{
			Method: r.Method,
			Auth:   r.Header.Get("Authorization"),
			Accept: r.Header.Get("Accept"),
			Type:   r.Header.Get("Content-Type"),
			Body:   body,
		})
	}))
	t.Cleanup(server.Close)

	var got echo
	answer, err := JSON(t.Context(), JSONParams{
		Client: server.Client(),
		Method: http.MethodPost,
		URL:    server.URL + "/ok",
		Header: http.Header{"Authorization": {"Bearer tok"}, "Content-Type": {"application/vnd.weave+json"}},
		Body:   map[string]int{"n": 1},
	}, &got)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, answer.Status)
	assert.True(t, answer.IsSuccess())
	assert.Equal(t, "POST", got.Method)
	assert.Equal(t, "Bearer tok", got.Auth)
	assert.Equal(t, "application/json", got.Accept)
	assert.Equal(t, "application/vnd.weave+json", got.Type)
	assert.JSONEq(t, `{"n":1}`, string(got.Body))

	var ignored echo
	refused, err := JSON(t.Context(), JSONParams{
		Client: server.Client(),
		Method: http.MethodGet,
		URL:    server.URL + "/refuse",
	}, &ignored)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, refused.Status)
	assert.False(t, refused.IsSuccess())
	assert.JSONEq(t, `{"message":"Bad credentials"}`, string(refused.Body))
	assert.Empty(t, ignored.Method)
}

// TestJSONCapsTheBody covers a body longer than the cap, which is cut rather than read whole.
func TestJSONCapsTheBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, 100))
	}))
	t.Cleanup(server.Close)

	answer, err := JSON(t.Context(), JSONParams{
		Client:   server.Client(),
		Method:   http.MethodGet,
		URL:      server.URL,
		MaxBytes: 10,
	}, nil)
	require.NoError(t, err)
	assert.Len(t, answer.Body, 10)
}

// TestJSONReportsAnUnreachableHost covers the transport failing, which is an error rather than
// an answer.
func TestJSONReportsAnUnreachableHost(t *testing.T) {
	t.Parallel()

	_, err := JSON(t.Context(), JSONParams{Method: http.MethodGet, URL: "http://127.0.0.1:1/"}, nil)
	require.Error(t, err)
}

// TestLocalClientOutwaitsAHandler covers a unary handler on weave's own server holding its
// response headers past the bound an outside call gets, which the local client waits out.
func TestLocalClientOutwaitsAHandler(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(responseHeaderTimeout + time.Second):
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(server.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	resp, err := NewLocalClient().Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
