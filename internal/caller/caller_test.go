package caller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFromHeader covers reading an identity off a request, including the shapes a CLI
// produces when it has nothing to fill in.
func TestFromHeader(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		want    Caller
	}{
		{
			name:    "an agent naming itself",
			headers: map[string]string{AgentHeader: "agent-1", SessionHeader: "sess-1"},
			want:    Caller{AgentID: "agent-1", SessionID: "sess-1"},
		},
		{
			name: "the panel, which sends nothing",
		},
		{
			name:    "a variable that was not set, which the CLI sends as an empty header",
			headers: map[string]string{AgentHeader: "", SessionHeader: ""},
		},
		{
			name:    "a variable the CLI passed through without expanding it",
			headers: map[string]string{AgentHeader: "${KOMRAD_AGENT_ID}"},
		},
		{
			name:    "an agent with no conversation yet",
			headers: map[string]string{AgentHeader: "agent-2"},
			want:    Caller{AgentID: "agent-2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := http.Header{}
			for key, value := range tt.headers {
				header.Set(key, value)
			}

			from := FromHeader(header)
			assert.Equal(t, tt.want, from)
			assert.Equal(t, tt.want != Caller{}, from.IsKnown())
		})
	}
}

// TestSetHeaderLeavesOutWhatIsNotKnown covers a caller that named no agent writing no header at
// all.
func TestSetHeaderLeavesOutWhatIsNotKnown(t *testing.T) {
	header := http.Header{}
	Caller{AgentID: "agent-1"}.SetHeader(header)

	assert.Equal(t, "agent-1", header.Get(AgentHeader))
	_, hasSession := header[http.CanonicalHeaderKey(SessionHeader)]
	assert.False(t, hasSession)
}

// TestTransportForwardsTheServedRequest covers a call the service makes on somebody's
// behalf, which must arrive as the agent that asked rather than as the service itself.
func TestTransportForwardsTheServedRequest(t *testing.T) {
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
	}))
	defer server.Close()

	ctx := With(WithTrace(t.Context(), "trace-9"), Caller{AgentID: "agent-1", SessionID: "sess-1"})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, nil)
	require.NoError(t, err)

	res, err := (&http.Client{Transport: Transport(nil)}).Do(req)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())

	assert.Equal(t, "agent-1", got.Get(AgentHeader))
	assert.Equal(t, "sess-1", got.Get(SessionHeader))
	assert.Equal(t, "trace-9", got.Get(TraceHeader))
}

// TestTransportLeavesAnUnattributedCallAlone covers the panel's own calls and anything the
// service does outside a request, which go as they were built.
func TestTransportLeavesAnUnattributedCallAlone(t *testing.T) {
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
	}))
	defer server.Close()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, nil)
	require.NoError(t, err)

	res, err := (&http.Client{Transport: Transport(nil)}).Do(req)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())

	assert.Empty(t, got.Get(AgentHeader))
	assert.Empty(t, got.Get(TraceHeader))
}

// TestTransportDoesNotEditTheCallersRequest covers the rule a RoundTripper works under: the
// request belongs to whoever built it and must not be stamped in place.
func TestTransportDoesNotEditTheCallersRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()

	ctx := With(t.Context(), Caller{AgentID: "agent-1"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, nil)
	require.NoError(t, err)

	res, err := (&http.Client{Transport: Transport(nil)}).Do(req)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())

	assert.Empty(t, req.Header.Get(AgentHeader), "the request handed in is left as it was")
}

// TestContextCarriesTheIdentity covers what a handler sees on its context, inside and
// outside a request.
func TestContextCarriesTheIdentity(t *testing.T) {
	from := Caller{AgentID: "agent-1", SessionID: "sess-1"}
	ctx := With(WithTrace(t.Context(), "trace-9"), from)

	assert.Equal(t, from, From(ctx))
	assert.Equal(t, "trace-9", Trace(ctx))

	assert.Equal(t, Caller{}, From(t.Context()))
	assert.Empty(t, Trace(t.Context()))
}
