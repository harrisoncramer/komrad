// Package codex runs Codex as a weave agent, building the command that starts or resumes a
// conversation, reading its rollout log, and telling from a captured frame whether it is
// working, waiting on a person, or idle.
//
// It also owns the trust file, since Codex refuses to run in a directory it has not been told
// about and every worktree weave makes is a directory it has never seen.
package codex

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"weave/internal/harness"
)

const binary = "codex"

const originatorEnv = "CODEX_INTERNAL_ORIGINATOR_OVERRIDE"

func originator(agentID string) string { return "weave-" + agentID }

func originatorPrefix(agentID string) string {
	if agentID == "" {
		return ""
	}

	return fmt.Sprintf("%s=%s ", originatorEnv, harness.QuoteArg(originator(agentID)))
}

type Harness struct {
	sessionsDir     string
	configPath      string
	trustLaunchDirs bool
}

type NewHarnessParams struct {
	SessionsDir     string
	ConfigPath      string
	TrustLaunchDirs bool
}

// NewHarness builds a harness from opts.
func NewHarness(opts NewHarnessParams) *Harness {
	if opts.SessionsDir == "" {
		opts.SessionsDir = DefaultSessionsDir()
	}
	if opts.ConfigPath == "" {
		opts.ConfigPath = DefaultConfigPath()
	}

	return &Harness{
		sessionsDir:     opts.SessionsDir,
		configPath:      opts.ConfigPath,
		trustLaunchDirs: opts.TrustLaunchDirs,
	}
}

var _ harness.Harness = (*Harness)(nil)

// ID is the name an agent running Codex is recorded under.
func (h *Harness) ID() string { return binary }

// Binary is the CLI a pane's shell has to resolve to start an agent.
func (h *Harness) Binary() string { return binary }

// InterruptKeys stops Codex where it stands, which is one Escape since it has no second
// prompt to dismiss the way Claude does.
func (h *Harness) InterruptKeys() []string { return []string{"Escape"} }

// MintSessionID returns empty, since Codex takes no session id on the command line and
// names the conversation itself, which the panel adopts afterwards through FindSession.
func (h *Harness) MintSessionID() string { return "" }

// DefaultSessionsDir is where Codex records its rollouts, returning a relative path that
// matches no rollouts when the home directory cannot be read.
func DefaultSessionsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	return filepath.Join(home, ".codex", "sessions")
}

type sessionMeta struct {
	ID         string `json:"id"`
	Originator string `json:"originator"`
}

// FindSession returns the conversation Codex started for this agent, or empty when none has
// been written yet, since Codex records nothing until the first turn is submitted.
func (h *Harness) FindSession(agentID string) (string, error) {
	if agentID == "" {
		return "", nil
	}

	paths, err := filepath.Glob(filepath.Join(h.sessionsDir, "*", "*", "*", "rollout-*.jsonl"))
	if err != nil {
		return "", fmt.Errorf("failed to list codex rollouts: %w", err)
	}

	slices.Sort(paths)

	// Anything else a rollout could be matched on is shared with a Codex started by hand in the
	// same place.
	want := originator(agentID)
	// The newest match wins, since an agent relaunched before its first conversation was adopted
	// leaves an abandoned rollout carrying the same stamp.
	for _, path := range slices.Backward(paths) {
		meta, err := readSessionMeta(path)
		if err != nil || meta.ID == "" || meta.Originator != want {
			continue
		}

		return meta.ID, nil
	}

	return "", nil
}

func readSessionMeta(path string) (sessionMeta, error) {
	file, err := os.Open(path)
	if err != nil {
		return sessionMeta{}, fmt.Errorf("failed to open rollout %q: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	var record struct {
		Type    string      `json:"type"`
		Payload sessionMeta `json:"payload"`
	}
	if err := json.NewDecoder(file).Decode(&record); err != nil {
		return sessionMeta{}, fmt.Errorf("failed to read the opening record of %q: %w", path, err)
	}
	if record.Type != "session_meta" {
		return sessionMeta{}, fmt.Errorf("rollout %q opens with %q rather than session_meta", path, record.Type)
	}

	return record.Payload, nil
}
