package codex

import (
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"komrad/internal/harness"
	"komrad/internal/harnesstest"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChannelSend covers how the channel hands text to an agent's thread: a steer into a running
// turn, the queue otherwise, and the false it returns when nothing took the text.
func TestChannelSend(t *testing.T) {
	agentID := uuid.New()

	opening := []string{"initialize", "initialized", "thread/turns/list"}

	tests := []struct {
		name        string
		sessionID   string
		noServer    bool
		status      string
		refuse      []string
		want        bool
		wantMethods []string
	}{
		{
			name:     "an agent whose thread komrad has not learned",
			noServer: true,
		},
		{
			name:      "no app server listening",
			sessionID: "thread-1",
			noServer:  true,
		},
		{
			name:        "a running turn is steered",
			sessionID:   "thread-1",
			status:      turnInProgress,
			want:        true,
			wantMethods: append(opening, "turn/steer"),
		},
		{
			name:        "an idle thread gets the text queued, which starts a turn",
			sessionID:   "thread-1",
			status:      "completed",
			want:        true,
			wantMethods: append(opening, "thread/queue/add"),
		},
		{
			name:        "a steer refused as the turn ends falls through to the queue",
			sessionID:   "thread-1",
			status:      turnInProgress,
			refuse:      []string{"turn/steer"},
			want:        true,
			wantMethods: append(opening, "turn/steer", "thread/queue/add"),
		},
		{
			name:        "a queue the app server refuses took nothing",
			sessionID:   "thread-1",
			status:      "completed",
			refuse:      []string{"thread/queue/add"},
			wantMethods: append(opening, "thread/queue/add"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hn := NewHarness(NewHarnessParams{Home: t.TempDir(), CacheDir: harnesstest.SocketDir(t)})

			server := &appServer{status: tt.status, refuse: tt.refuse}
			if !tt.noServer {
				path, err := harness.PrepareSocket(hn.cacheDir, HarnessID, agentID)
				require.NoError(t, err)
				server.listen(t, path)
			}

			sent, err := hn.Channel().Send(t.Context(), harness.Target{AgentID: agentID, SessionID: tt.sessionID}, "read your mail")
			require.NoError(t, err)
			assert.Equal(t, tt.want, sent)
			assert.Equal(t, tt.wantMethods, server.received())
		})
	}
}

// appServer answers the calls the channel makes the way Codex's app server does, saying something
// unasked before each answer as the real one does.
type appServer struct {
	status  string
	refuse  []string
	mu      sync.Mutex
	methods []string
}

func (s *appServer) listen(t *testing.T, path string) {
	t.Helper()

	listener, err := net.Listen("unix", path)
	require.NoError(t, err)

	server := &http.Server{Handler: http.HandlerFunc(s.serve), ReadHeaderTimeout: time.Second}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
}

func (s *appServer) serve(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()

	for {
		_, raw, err := conn.Read(r.Context())
		if err != nil {
			return
		}

		var request struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal(raw, &request); err != nil {
			return
		}

		s.mu.Lock()
		s.methods = append(s.methods, request.Method)
		s.mu.Unlock()

		if request.ID == nil {
			continue
		}

		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"method":"thread/status/changed","params":{}}`))

		reply := map[string]any{"id": *request.ID, "result": s.result(request.Method)}
		for _, refused := range s.refuse {
			if refused == request.Method {
				reply = map[string]any{"id": *request.ID, "error": map[string]any{"code": -32600, "message": "refused"}}
			}
		}

		body, err := json.Marshal(reply)
		if err != nil {
			return
		}
		_ = conn.Write(r.Context(), websocket.MessageText, body)
	}
}

func (s *appServer) result(method string) any {
	if method == "thread/turns/list" {
		return map[string]any{"data": []map[string]string{{"id": "turn-1", "status": s.status}}}
	}

	return map[string]any{}
}

func (s *appServer) received() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.methods
}
