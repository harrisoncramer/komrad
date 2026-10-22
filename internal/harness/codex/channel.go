package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"syscall"
	"time"

	"komrad/internal/harness"
	"komrad/lib/xhttp"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

const minVersion = "0.149.0"

const dialTimeout = time.Second

// sendTimeout bounds one whole exchange with the app server, from the dial to the last answer.
const sendTimeout = 10 * time.Second

const turnInProgress = "inProgress"

// MinVersion is the first Codex whose app server takes a message queued for a thread.
func (h *Harness) MinVersion() string {
	return minVersion
}

// Channel hands text to an agent's thread through the app server its pane runs.
func (h *Harness) Channel() harness.Channel {
	return channel{cacheDir: h.cacheDir}
}

type channel struct {
	cacheDir string
}

// Send steers the turn the agent's thread is in, or queues the text when no turn is running.
func (c channel) Send(ctx context.Context, target harness.Target, text string) (bool, error) {
	if target.SessionID == "" {
		return false, nil
	}

	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	conn, err := c.dial(ctx, target.AgentID)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to reach the codex app server for agent %q: %w", target.AgentID, err)
	}
	defer func() { _ = conn.CloseNow() }() //nolint:errcheck // the send already has its answer

	rpc := &rpcConn{conn: conn}
	if err := rpc.open(ctx); err != nil {
		return false, fmt.Errorf("failed to open a connection to the codex app server for agent %q: %w", target.AgentID, err)
	}

	return rpc.deliver(ctx, target.SessionID, text)
}

func (c channel) dial(ctx context.Context, agentID uuid.UUID) (*websocket.Conn, error) {
	path := harness.SocketPath(c.cacheDir, HarnessID, agentID)

	transport := xhttp.NewTransport()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		dialer := net.Dialer{Timeout: dialTimeout}

		return dialer.DialContext(ctx, "unix", path)
	}

	conn, response, err := websocket.Dial(ctx, "ws://localhost/", &websocket.DialOptions{HTTPClient: xhttp.NewClientOver(transport, 0)})
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return nil, err
	}

	return conn, nil
}

// rpcConn is one connection's JSON-RPC exchange with the app server.
type rpcConn struct {
	conn   *websocket.Conn
	lastID int
}

type rpcRequest struct {
	ID     *int   `json:"id,omitempty"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type rpcReply struct {
	ID     *int            `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("%s (%d)", e.Message, e.Code)
}

type userInput struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type turn struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// open introduces komrad to the app server, asking for the methods it marks experimental.
func (r *rpcConn) open(ctx context.Context) error {
	params := map[string]any{
		"clientInfo":   map[string]string{"name": "komrad", "version": "1"},
		"capabilities": map[string]bool{"experimentalApi": true},
	}
	if err := r.call(ctx, "initialize", params, nil); err != nil {
		return err
	}

	return r.write(ctx, rpcRequest{Method: "initialized"})
}

// deliver steers the thread's running turn, falling through to its queue when there is none or
// the turn ended before the steer landed.
func (r *rpcConn) deliver(ctx context.Context, threadID, text string) (bool, error) {
	input := []userInput{{Type: "text", Text: text}}

	var page struct {
		Data []turn `json:"data"`
	}
	params := map[string]any{"threadId": threadID, "limit": 1, "itemsView": "notLoaded"}
	if err := r.call(ctx, "thread/turns/list", params, &page); err != nil {
		return false, fmt.Errorf("failed to read the turns of thread %q: %w", threadID, err)
	}

	if len(page.Data) > 0 && page.Data[0].Status == turnInProgress {
		steer := map[string]any{"threadId": threadID, "expectedTurnId": page.Data[0].ID, "input": input}
		err := r.call(ctx, "turn/steer", steer, nil)
		if err == nil {
			return true, nil
		}

		var refused *rpcError
		if !errors.As(err, &refused) {
			return true, fmt.Errorf("failed to steer thread %q: %w", threadID, err)
		}
	}

	queue := map[string]any{"threadId": threadID, "clientUserMessageId": uuid.NewString(), "input": input}
	err := r.call(ctx, "thread/queue/add", queue, nil)

	var refused *rpcError
	if errors.As(err, &refused) {
		return false, nil
	}
	if err != nil {
		return true, fmt.Errorf("failed to queue a message on thread %q: %w", threadID, err)
	}

	return true, nil
}

// call sends one request and waits for its answer, passing over whatever the app server says in
// between.
func (r *rpcConn) call(ctx context.Context, method string, params, result any) error {
	r.lastID++
	id := r.lastID
	if err := r.write(ctx, rpcRequest{ID: &id, Method: method, Params: params}); err != nil {
		return err
	}

	for {
		_, raw, err := r.conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("failed to read the answer to %s: %w", method, err)
		}

		var reply rpcReply
		if err := json.Unmarshal(raw, &reply); err != nil {
			return fmt.Errorf("failed to decode the answer to %s: %w", method, err)
		}
		if reply.ID == nil || *reply.ID != id || reply.Method != "" {
			continue
		}
		if reply.Error != nil {
			return reply.Error
		}
		if result == nil {
			return nil
		}

		if err := json.Unmarshal(reply.Result, result); err != nil {
			return fmt.Errorf("failed to decode the answer to %s: %w", method, err)
		}

		return nil
	}
}

func (r *rpcConn) write(ctx context.Context, request rpcRequest) error {
	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to encode %s: %w", request.Method, err)
	}
	if err := r.conn.Write(ctx, websocket.MessageText, body); err != nil {
		return fmt.Errorf("failed to send %s: %w", request.Method, err)
	}

	return nil
}
