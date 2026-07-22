// Package registry persists the state of Claude agents, one file per session,
// under a directory shared with the Claude Code hooks.
package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// State is the lifecycle stage of an agent.
type State string

const (
	StateThinking State = "thinking"
	StateDone     State = "done"
	StateNew      State = "new"
)

// Agent is one registered Claude session.
type Agent struct {
	File    string
	Session string
	Pane    string
	State   State
	Msg     string
}

// Store reads and writes agent entries under a directory.
type Store struct {
	dir string
}

// New returns a Store rooted at dir.
func New(dir string) *Store {
	return &Store{dir: dir}
}

// SanitizeSessionName maps a name to the characters tmux allows in a session
// name, which is also the registry file name so the two always agree.
func SanitizeSessionName(name string) string {
	return strings.NewReplacer(":", "-", ".", "-", " ", "-").Replace(name)
}

func (s *Store) path(session string) string {
	return filepath.Join(s.dir, SanitizeSessionName(session))
}

// Write persists an agent, keyed by its session name.
func (s *Store) Write(a Agent) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("failed to create registry dir: %w", err)
	}

	entry := fmt.Sprintf("SESSION=%s\nPANE=%s\nSTATE=%s\nMSG=%s\n", a.Session, a.Pane, a.State, a.Msg)
	if err := os.WriteFile(s.path(a.Session), []byte(entry), 0o644); err != nil {
		return fmt.Errorf("failed to write agent %q: %w", a.Session, err)
	}

	return nil
}

// Get returns the agent for a session, or nil when none is registered.
func (s *Store) Get(session string) (*Agent, error) {
	agent, err := parse(s.path(session))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read agent %q: %w", session, err)
	}

	return agent, nil
}

// List returns every registered agent, sorted by session name.
func (s *Store) List() ([]Agent, error) {
	entries, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read registry dir: %w", err)
	}

	agents := make([]Agent, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		agent, err := parse(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			continue
		}

		agents = append(agents, *agent)
	}

	return agents, nil
}

// Remove deletes an agent's entry.
func (s *Store) Remove(a Agent) error {
	if err := os.Remove(a.File); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove agent %q: %w", a.Session, err)
	}

	return nil
}

func parse(path string) (*Agent, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	agent := Agent{File: path}
	for line := range strings.SplitSeq(string(contents), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		switch key {
		case "SESSION":
			agent.Session = value
		case "PANE":
			agent.Pane = value
		case "STATE":
			agent.State = State(value)
		case "MSG":
			agent.Msg = value
		}
	}

	return &agent, nil
}
