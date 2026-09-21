// Package codex runs Codex as a weave agent, building the command that starts or resumes a
// conversation, reading its rollout log, and telling from a captured frame whether it is
// working, waiting on a person, or idle.
//
// It also owns the trust file.
package codex

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"weave/config"
	"weave/internal/harness"
)

const binary = "codex"

const originatorEnv = "CODEX_INTERNAL_ORIGINATOR_OVERRIDE"

const homeEnv = "CODEX_HOME"

func originator(agentID string) string {
	return "weave-" + agentID
}

func originatorPrefix(agentID string) string {
	if agentID == "" {
		return ""
	}

	return fmt.Sprintf("%s=%s ", originatorEnv, harness.QuoteArg(originator(agentID)))
}

type Harness struct {
	home            string
	trustLaunchDirs bool
}

type NewHarnessParams struct {
	Home            string
	TrustLaunchDirs bool
}

// NewHarness builds a harness from opts.
func NewHarness(params NewHarnessParams) *Harness {
	return &Harness{
		home:            params.Home,
		trustLaunchDirs: params.TrustLaunchDirs,
	}
}

var _ harness.Harness = (*Harness)(nil)

// ID is the name an agent running Codex is recorded under.
func (h *Harness) ID() string {
	return binary
}

// Binary is the CLI a pane's shell has to resolve to start an agent.
func (h *Harness) Binary() string {
	return binary
}

// InterruptKeys stops Codex where it stands.
func (h *Harness) InterruptKeys() []string {
	return []string{"Escape"}
}

// MintSessionID returns empty, Codex taking no session id on the command line and naming the
// conversation itself.
func (h *Harness) MintSessionID() string {
	return ""
}

// NamedHome reads the directory the environment names in place of ~/.codex, empty when it
// names none.
func NamedHome() string {
	dir, _ := config.LookupEnv(homeEnv)

	return dir
}

func (h *Harness) homePrefix() string {
	if h.home == "" {
		return ""
	}

	return homeEnv + "=" + harness.QuoteArg(h.home) + " "
}

func (h *Harness) resolveHome() (string, error) {
	if h.home != "" {
		return h.home, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	return filepath.Join(home, ".codex"), nil
}

func (h *Harness) resolveSessionsDir() (string, error) {
	home, err := h.resolveHome()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, "sessions"), nil
}

type sessionMeta struct {
	ID         string `json:"id"`
	Originator string `json:"originator"`
}

// FindSession returns the conversation Codex started for this agent, or empty until the first
// turn is submitted.
func (h *Harness) FindSession(agentID string) (string, error) {
	if agentID == "" {
		return "", nil
	}

	sessionsDir, err := h.resolveSessionsDir()
	if err != nil {
		return "", err
	}

	paths, err := filepath.Glob(filepath.Join(sessionsDir, "*", "*", "*", "rollout-*.jsonl"))
	if err != nil {
		return "", fmt.Errorf("failed to list codex rollouts: %w", err)
	}

	slices.Sort(paths)

	// Anything else a rollout could be matched on is shared with a Codex started by hand in the
	// same place.
	want := originator(agentID)
	// The newest match wins, an agent relaunched before its first conversation was adopted leaving
	// an abandoned rollout carrying the same stamp.
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
