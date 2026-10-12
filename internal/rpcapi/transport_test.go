package rpcapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type namedTransport string

func (n namedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Transport": {string(n)}},
		Body:       http.NoBody,
	}, nil
}

// TestTransportRoutesByProcedure covers a call that waits on hooks going over the transport with no header timeout, wherever the server is mounted.
func TestTransportRoutesByProcedure(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "a call that waits on hooks",
			url:  "http://engine/agent.v1.AgentService/CreateAgent",
			want: "awaiting",
		},
		{
			name: "one under a mount path",
			url:  "https://engine/komrad/task.v1.TaskService/ArchiveTask",
			want: "awaiting",
		},
		{
			name: "an ordinary call",
			url:  "http://engine/agent.v1.AgentService/ListAgents",
			want: "plain",
		},
		{
			name: "a method another service shares the name of",
			url:  "http://engine/other.v1.OtherService/CreateAgent",
			want: "plain",
		},
	}

	transport := hookAwareTransport{plain: namedTransport("plain"), awaiting: namedTransport("awaiting")}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := transport.RoundTrip(httptest.NewRequest(http.MethodPost, tt.url, http.NoBody))
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()

			assert.Equal(t, tt.want, resp.Header.Get("Transport"))
		})
	}
}
