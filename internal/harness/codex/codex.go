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
	sessionsDir string
	configPath  string

	trustLaunchDirs bool
}

type NewHarnessParams struct {
	SessionsDir string
	ConfigPath  string

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

// GetID is the name an agent running Codex is recorded under.
func (h *Harness) GetID() string { return binary }

// GetBinary is the CLI a pane's shell has to resolve to start an agent.
func (h *Harness) GetBinary() string { return binary }

// GetInterruptKeys stops Codex where it stands. It reads one Escape as an interrupt and has
// no second prompt to dismiss, unlike Claude, which is the reason these keys are per-harness
// rather than one pair the panel sends to everything.
func (h *Harness) GetInterruptKeys() []string { return []string{"Escape"} }

// MintSessionID returns empty, since Codex takes no session id on the command line and
// names the conversation itself, which the panel adopts afterwards through FindSession.
func (h *Harness) MintSessionID() string { return "" }

// DefaultSessionsDir is where Codex records its rollouts. A home that cannot be
// read yields a relative path, which matches no rollouts rather than erroring.
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

// FindSession returns the conversation Codex started for this agent, or empty when none
// has been written yet, since Codex records nothing until the first turn is submitted.
//
// The match is the originator the launch stamped on the rollout, which is this agent's id
// and nothing else's. Anything else a rollout could be matched on is shared with a Codex
// started by hand in the same place, so it could adopt someone else's conversation.
//
// The newest match wins, since an agent relaunched before its first conversation was
// adopted leaves an abandoned rollout carrying the same stamp.
func (h *Harness) FindSession(agentID string) (string, error) {
	if agentID == "" {
		return "", nil
	}

	paths, err := filepath.Glob(filepath.Join(h.sessionsDir, "*", "*", "*", "rollout-*.jsonl"))
	if err != nil {
		return "", fmt.Errorf("failed to list codex rollouts: %w", err)
	}

	slices.Sort(paths)

	want := originator(agentID)
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

// Relocate has nothing to carry, since Codex resumes a conversation by id from anywhere.
func (h *Harness) Relocate(string, string) error { return nil }
