//go:build integration

package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"weave/config"
	"weave/internal/caller"
	"weave/internal/errui"
	"weave/internal/server"
	"weave/internal/version"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRefusalReadsWhatTheServiceSaid pins how a call that was refused reaches the person who
// typed it.
func TestRefusalReadsWhatTheServiceSaid(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		status  string
		want    string
	}{
		{
			name:    "connect's own shape, which carries both",
			payload: `{"code":"not_found","message":"agent \"alpha\" is not one this panel holds"}`,
			status:  "404 Not Found",
			want:    `not_found: agent "alpha" is not one this panel holds`,
		},
		{
			name:    "a message with no code",
			payload: `{"message":"the request was not valid"}`,
			status:  "400 Bad Request",
			want:    "the request was not valid",
		},
		{
			name:    "a body that is not the service talking, which is kept rather than dropped",
			payload: "502 Bad Gateway\nupstream closed",
			status:  "502 Bad Gateway",
			want:    "502 Bad Gateway: 502 Bad Gateway\nupstream closed",
		},
		{
			name:    "json carrying no message at all",
			payload: `{"code":"internal"}`,
			status:  "500 Internal Server Error",
			want:    `500 Internal Server Error: {"code":"internal"}`,
		},
		{
			name:    "nothing at all, where the status is the whole story",
			payload: "   ",
			status:  "503 Service Unavailable",
			want:    "503 Service Unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := readRefusal([]byte(tt.payload), tt.status)
			require.Error(t, err)
			assert.Equal(t, tt.want, err.Error())
		})
	}
}

// TestResponseIsWrittenForWhoeverIsReadingIt pins the two shapes the response comes out in, one
// for a person at a terminal and one for a script piping it somewhere.
func TestResponseIsWrittenForWhoeverIsReadingIt(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		compact bool
		want    string
	}{
		{
			name:    "indented by default",
			payload: `{"agent":{"id":"a1"}}`,
			want:    "{\n  \"agent\": {\n    \"id\": \"a1\"\n  }\n}\n",
		},
		{
			name:    "on one line when asked for",
			payload: `{"agent":{"id":"a1"}}`,
			compact: true,
			want:    "{\"agent\":{\"id\":\"a1\"}}\n",
		},
		{
			name:    "a payload that will not indent is passed through as it came",
			payload: "not json at all",
			want:    "not json at all\n",
		},
		{
			name:    "an empty response still ends its line",
			payload: "",
			want:    "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			require.NoError(t, writeResponse(&out, []byte(tt.payload), tt.compact))
			assert.Equal(t, tt.want, out.String())
		})
	}
}

// TestCallCarriesWhoIsAskingAndWhatTheyAskedFor pins the request as the service receives it.
func TestCallCarriesWhoIsAskingAndWhatTheyAskedFor(t *testing.T) {
	var (
		gotBody   map[string]any
		gotHeader http.Header
		gotPath   string
	)

	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotHeader = r.URL.Path, r.Header.Clone()
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)

		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(service.Close)

	payload, err := callRPC(t.Context(), callRPCParams{
		Addr:    strings.TrimPrefix(service.URL, "http://"),
		Path:    "/agent.v1.AgentService/GetAgent",
		From:    caller.Caller{AgentID: "agent-one", SessionID: "session-one"},
		Request: map[string]any{"id": "a1"},
	})
	require.NoError(t, err)

	assert.JSONEq(t, `{"ok":true}`, string(payload))
	assert.Equal(t, "/agent.v1.AgentService/GetAgent", gotPath)
	assert.Equal(t, map[string]any{"id": "a1"}, gotBody)
	assert.Equal(t, "application/json", gotHeader.Get("Content-Type"))
	assert.Equal(t, "agent-one", gotHeader.Get(caller.AgentHeader))
	assert.Equal(t, "session-one", gotHeader.Get(caller.SessionHeader))
}

// TestCallReportsWhatWentWrongRatherThanTheBody pins the two ways a call fails short of an
// answer: a service that refused it, and a server that was not there to ask.
func TestCallReportsWhatWentWrongRatherThanTheBody(t *testing.T) {
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"not_found","message":"no agent holds that id"}`))
	}))
	t.Cleanup(refusing.Close)

	_, err := callRPC(t.Context(), callRPCParams{
		Addr: strings.TrimPrefix(refusing.URL, "http://"),
		Path: "/agent.v1.AgentService/GetAgent",
	})
	require.Error(t, err)
	assert.Equal(t, "not_found: no agent holds that id", err.Error())

	_, err = callRPC(t.Context(), callRPCParams{Addr: deadAddr(t), Path: "/agent.v1.AgentService/GetAgent"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to reach the server", "the address is named rather than the socket error alone")
}

// TestRequireServerSaysHowToStartOne pins the message a script gets when the panel is not up.
func TestRequireServerSaysHowToStartOne(t *testing.T) {
	var healthPath string
	answering := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		healthPath = r.URL.Path
		_, _ = w.Write([]byte(`{"version":"` + version.Version() + `"}`))
	}))
	t.Cleanup(answering.Close)

	addr := strings.TrimPrefix(answering.URL, "http://")
	require.NoError(t, requireServer(t.Context(), addr, addr))
	assert.Equal(t, server.HealthPath, healthPath, "the probe asks the health route rather than the call's own")

	dead := deadAddr(t)

	err := requireServer(t.Context(), dead, dead)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "weave start", "the refusal says what to do about it")

	err = requireServer(t.Context(), dead, "127.0.0.1:8787")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "127.0.0.1:8787", "a forwarded engine is named by the address it was configured with")
}

// TestRequireServerRefusesAnotherBuild covers a server left running from an older build, which
// would answer a call this binary makes with unimplemented or, worse, with something else.
func TestRequireServerRefusesAnotherBuild(t *testing.T) {
	stale := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":"v0.0.0-20260101000000-0123456789ab","commit":"0123456"}`))
	}))
	t.Cleanup(stale.Close)

	addr := strings.TrimPrefix(stale.URL, "http://")
	err := requireServer(t.Context(), addr, addr)
	require.ErrorIs(t, err, ErrServerBuild)
	assert.Contains(t, err.Error(), "0123456", "the build that is serving is named")
	assert.Contains(t, err.Error(), "weave stop", "a local server is restarted")

	err = requireServer(t.Context(), addr, "127.0.0.1:8787")
	require.ErrorIs(t, err, errui.ErrEngineBuild, "a forwarded engine is somebody else's build to match")
	assert.NotContains(t, err.Error(), "weave start", "starting a server here would not be the engine")
}

// deadAddr is an address nothing is listening on, taken by opening a server and closing it so
// the port is one the operating system just handed out rather than one guessed at.
func deadAddr(t *testing.T) string {
	t.Helper()

	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closed.Close()

	return strings.TrimPrefix(closed.URL, "http://")
}

// TestRpcSubcommandCarriesItsFlagsToTheService walks the whole path a script takes, from flags
// derived from the protos by reflection to the field the schema names.
func TestRpcSubcommandCarriesItsFlagsToTheService(t *testing.T) {
	var got map[string]any

	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == server.HealthPath {
			_, _ = w.Write([]byte(`{"version":"` + version.Version() + `"}`))

			return
		}

		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)

		_, _ = w.Write([]byte(`{"agent":{"id":"a1","name":"alpha"}}`))
	}))
	t.Cleanup(service.Close)

	addr := strings.TrimPrefix(service.URL, "http://")
	root := rpcCmd(&app{env: &config.Environment{Addr: addr, AgentID: "agent-one"}})

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"get-agent", "--id", "a1", "--compact"})

	require.NoError(t, root.ExecuteContext(t.Context()))

	assert.Equal(t, map[string]any{"id": "a1"}, got, "the flag arrives as the field the schema names")
	assert.JSONEq(t, `{"agent":{"id":"a1","name":"alpha"}}`, out.String())
	assert.NotContains(t, strings.TrimSpace(out.String()), "\n", "--compact keeps it to one line")
}

// TestRpcSubcommandRefusalReachesTheCaller pins that a service saying no comes back as the
// command failing with what it said.
func TestRpcSubcommandRefusalReachesTheCaller(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == server.HealthPath {
			_, _ = w.Write([]byte(`{"version":"` + version.Version() + `"}`))

			return
		}

		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"not_found","message":"no agent holds that id"}`))
	}))
	t.Cleanup(service.Close)

	addr := strings.TrimPrefix(service.URL, "http://")
	root := rpcCmd(&app{env: &config.Environment{Addr: addr}})

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"get-agent", "--id", "a1"})

	err := root.ExecuteContext(t.Context())
	require.Error(t, err)
	assert.Equal(t, "not_found: no agent holds that id", err.Error())
	assert.Empty(t, out.String(), "nothing is printed as though it were an answer")
}

// TestRpcSubcommandStopsWhenNothingIsServing pins that the call is not attempted at all.
func TestRpcSubcommandStopsWhenNothingIsServing(t *testing.T) {
	root := rpcCmd(&app{env: &config.Environment{Addr: deadAddr(t)}})

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"get-agent", "--id", "a1"})

	err := root.ExecuteContext(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "weave start")
}
