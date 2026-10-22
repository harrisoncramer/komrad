package claude

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
)

const minVersion = "2.1.224"

const dialTimeout = time.Second

const writeTimeout = time.Second

// MinVersion is the first Claude Code that listens on a messaging socket.
func (h *Harness) MinVersion() string {
	return minVersion
}

// Channel writes to the messaging socket each agent's CLI listens on.
func (h *Harness) Channel() harness.Channel {
	return channel{cacheDir: h.cacheDir}
}

type channel struct {
	cacheDir string
}

type inboundMessage struct {
	Type    string         `json:"type"`
	Message inboundContent `json:"message"`
	From    string         `json:"from"`
}

type inboundContent struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Send writes text to the agent's messaging socket as a message from komrad.
func (c channel) Send(ctx context.Context, target harness.Target, text string) (bool, error) {
	dialer := net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "unix", harness.SocketPath(c.cacheDir, HarnessID, target.AgentID))
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to reach the messaging socket of agent %q: %w", target.AgentID, err)
	}
	defer func() { _ = conn.Close() }() //nolint:errcheck // the line was written or the send already failed

	line, err := json.Marshal(inboundMessage{
		Type:    "user",
		Message: inboundContent{Role: "user", Content: text},
		From:    "komrad",
	})
	if err != nil {
		return false, fmt.Errorf("failed to encode the message to agent %q: %w", target.AgentID, err)
	}

	if err := conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return false, fmt.Errorf("failed to write to the messaging socket of agent %q: %w", target.AgentID, err)
	}
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return false, fmt.Errorf("failed to write to the messaging socket of agent %q: %w", target.AgentID, err)
	}

	return true, nil
}
