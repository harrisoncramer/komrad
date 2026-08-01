// Package codex runs OpenAI's Codex CLI as a weave harness. Everything
// here is true of that CLI and of nothing else: the config overrides it takes, the
// chrome its TUI draws, and the rollout files it records a conversation in.
//
// Codex differs from Claude Code in the one way that shapes the rest. It will not
// be told which id to file a conversation under, so the panel cannot name one
// before it exists and has to adopt the id Codex chose after the launch.
package codex

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"weave/internal/harness"
)

// originatorEnv is what Codex calls the client that started a conversation, overridden
// per launch so the panel can recognize its own. Codex writes the value into the opening
// record of the rollout, which is the only place an agent's id and the conversation id
// Codex minted ever appear together.
//
// It is marked internal by Codex and could be renamed in any release. The cost of that
// is an agent whose transcript never syncs rather than a wrong conversation adopted,
// since a stamp that stops arriving matches nothing.
const originatorEnv = "CODEX_INTERNAL_ORIGINATOR_OVERRIDE"

// originator is the value originatorEnv carries for an agent. It is prefixed rather than
// the bare id so a rollout says which tool started it, which is what the value means to
// Codex and to anything else reading the file.
func originator(agentID string) string { return "weave-" + agentID }

// originatorPrefix is the assignment that goes ahead of the CLI on the command line, or
// nothing for a launch with no agent id to stamp.
func originatorPrefix(agentID string) string {
	if agentID == "" {
		return ""
	}

	return fmt.Sprintf("%s=%s ", originatorEnv, harness.QuoteArg(originator(agentID)))
}

// Harness puts Codex up in a pane and reads back what happened in it.
type Harness struct {
	// sessionsDir is Codex's $CODEX_HOME/sessions, where it writes one rollout file per
	// conversation under a year, month, and day of directories. configPath is the config
	// file beside it. Both are fields rather than constants so a test can point them at a
	// temp directory.
	sessionsDir string
	configPath  string
	// trustLaunchDirs answers Codex's directory-trust question in advance, which it can
	// only do by writing to the config above.
	trustLaunchDirs bool
}

// Options configures the Codex harness. The two paths default to Codex's own
// locations when they are left empty.
type Options struct {
	SessionsDir string
	ConfigPath  string
	// TrustLaunchDirs records each agent's directory as trusted before launching it.
	// Left off, an agent in a directory Codex has not seen before starts at the trust
	// question and shows in the panel as waiting.
	TrustLaunchDirs bool
}

// New builds a harness from opts.
func New(opts Options) *Harness {
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

// The panel reaches Codex only through the interface, so a method missing here is a
// compile error rather than a nil call on one of serve's loops.
var _ harness.Harness = (*Harness)(nil)

// GetID is the name an agent running Codex is recorded under.
func (h *Harness) GetID() string { return "codex" }

// MintSessionID returns empty. Codex takes no session id on the command line and
// names the conversation itself, so the panel adopts that name afterwards through
// FindSessions.
func (h *Harness) MintSessionID() string { return "" }

// DefaultSessionsDir is where Codex records its rollouts. An unresolvable home
// yields a relative path, which matches no rollouts rather than erroring.
func DefaultSessionsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	return filepath.Join(home, ".codex", "sessions")
}

// sessionMeta is the first record of a rollout, which names the conversation and
// carries the originator the launch stamped on it. Those two are what tie a file
// Codex named itself to the agent the panel launched.
type sessionMeta struct {
	ID         string `json:"id"`
	Originator string `json:"originator"`
}

// FindSession returns the conversation Codex started for this agent, or empty when
// none has been written yet. Codex records nothing until the first turn is submitted,
// so an agent still sitting at its prompt has no rollout at all and the next pass
// looks again.
//
// The match is the originator the launch stamped on the rollout, which is this
// agent's id and nothing else's. Everything about a rollout that a person would guess
// from — the directory it ran in, when it was written — is shared with any Codex a
// person started by hand in the same place, so a guess can adopt someone else's
// conversation. A stamp cannot.
//
// The newest match wins. An agent relaunched before its first conversation was
// adopted leaves an abandoned rollout carrying the same stamp, and the live pane is
// the later one.
func (h *Harness) FindSession(agentID string) (string, error) {
	if agentID == "" {
		return "", nil
	}

	paths, err := filepath.Glob(filepath.Join(h.sessionsDir, "*", "*", "*", "rollout-*.jsonl"))
	if err != nil {
		return "", fmt.Errorf("failed to list codex rollouts: %w", err)
	}

	// The filename opens with the timestamp Codex wrote it at, so sorting orders the
	// rollouts and the walk can run backwards to reach the newest first.
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

// readSessionMeta reads a rollout's opening record. A rollout is written as it goes,
// so a file caught before its first record is complete has nothing to read yet and
// is not an error.
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

// Relocate has nothing to carry. Codex resumes a conversation by id from anywhere,
// so a restore that has to run the agent somewhere else costs it nothing.
func (h *Harness) Relocate(string, string) error { return nil }
