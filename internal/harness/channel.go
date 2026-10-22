package harness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// maxSocketPath is the longest path a unix socket can be bound or dialled at on macOS, with room
// for the terminating zero.
const maxSocketPath = 103

// Target is the agent a channel speaks to or an observer reads.
type Target struct {
	AgentID   uuid.UUID
	Pane      string
	HarnessID string
	// SessionID is the conversation the agent's CLI is in, empty until komrad has learned it.
	SessionID string
}

// Channel is how komrad says something to an agent's CLI.
type Channel interface {
	// Send hands text to the agent's CLI, returning true when the text reached the CLI or may have
	// and false when it reached nothing, with an error saying what went wrong either way.
	Send(ctx context.Context, target Target, text string) (bool, error)
}

// SocketPath is where an agent's CLI listens for what komrad sends it.
func SocketPath(cacheDir, harnessID string, agentID uuid.UUID) string {
	sum := sha256.Sum256([]byte(agentID.String()))

	return filepath.Join(cacheDir, harnessID, hex.EncodeToString(sum[:])[:16]+".sock")
}

// PrepareSocket makes the directory an agent's socket goes in, refusing a path too long to bind.
func PrepareSocket(cacheDir, harnessID string, agentID uuid.UUID) (string, error) {
	path := SocketPath(cacheDir, harnessID, agentID)
	if len(path) > maxSocketPath {
		return "", fmt.Errorf("the socket %q is longer than the %d bytes a unix socket path can be", path, maxSocketPath)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("failed to create the socket directory: %w", err)
	}

	return path, nil
}

// Observer is how komrad learns what an agent is doing.
type Observer interface {
	Observe(ctx context.Context, target Target) (Observation, error)

	IsRunning(ctx context.Context, target Target) (bool, error)
}

// Observation is one reading of an agent, however it was taken.
type Observation struct {
	Activity Activity

	// Frame is the capture the reading was taken from, empty for a source that reads no screen.
	Frame string
}
