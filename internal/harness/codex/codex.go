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
	"time"

	"weave/internal/harness"
)

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
// the directory it was started in. Those two are what identify a rollout as one
// agent's, since nothing else ties a file Codex named to a row the panel holds.
type sessionMeta struct {
	ID  string `json:"id"`
	Cwd string `json:"cwd"`
}

// FindSessions lists the conversations Codex started in dir since notBefore, oldest
// first. It reads the first record of each candidate rollout rather than trusting
// the timestamp in the filename, since the directory a session ran in is the only
// thing that distinguishes one agent's conversation from another's.
func (h *Harness) FindSessions(dir string, notBefore time.Time) ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(h.sessionsDir, "*", "*", "*", "rollout-*.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("failed to list codex rollouts: %w", err)
	}

	// Oldest first, so a caller taking the first unclaimed one pairs the earliest
	// conversation with the earliest agent still missing an id.
	slices.Sort(paths)

	var found []string
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || info.ModTime().Before(notBefore) {
			continue
		}

		meta, err := readSessionMeta(path)
		if err != nil || meta.ID == "" || meta.Cwd != dir {
			continue
		}

		found = append(found, meta.ID)
	}

	return found, nil
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
