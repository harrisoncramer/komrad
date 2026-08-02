package codex

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// DefaultConfigPath is the config file Codex reads, which is also the only place it
// records which directories are trusted.
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	return filepath.Join(home, ".codex", "config.toml")
}

// trustDir records dir as trusted in Codex's own config, so an agent launched there
// does not stop at the question Codex asks the first time it runs somewhere.
//
// This writes to the user's Codex config because nothing else works. A -c override of
// the same key is ignored by the interactive UI, trust is not inherited from a parent
// directory, and even the flag that bypasses every approval leaves the question standing.
// A panel whose agents run in fresh worktrees would otherwise start each one blocked, so
// the user turns this on deliberately rather than getting it by default.
//
// An entry that already exists is left exactly as it is, including one the user set to
// untrusted, since a refusal recorded by hand outranks a launch.
func (h *Harness) trustDir(dir string) error {
	if !h.trustLaunchDirs || dir == "" || h.configPath == "" {
		return nil
	}

	current, err := os.ReadFile(h.configPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read the codex config %q: %w", h.configPath, err)
	}

	trusted, err := isDirListed(current, dir)
	if err != nil || trusted {
		return err
	}

	// Appended rather than re-encoded, so nothing the user wrote is reformatted or
	// dropped by a round trip through a parser that does not keep comments.
	grown := string(current)
	if grown != "" {
		grown += "\n"
	}
	grown += fmt.Sprintf("[projects.%s]\ntrust_level = %s\n", tomlString(dir), tomlString("trusted"))

	// The whole point of appending is not to break a file the user maintains by hand, so
	// what was built is parsed before it replaces anything.
	if err := toml.Unmarshal([]byte(grown), &map[string]any{}); err != nil {
		return fmt.Errorf("refusing to write a codex config that would not parse: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(h.configPath), 0o755); err != nil {
		return fmt.Errorf("failed to create the codex config dir: %w", err)
	}

	if err := os.WriteFile(h.configPath, []byte(grown), 0o644); err != nil {
		return fmt.Errorf("failed to write the codex config %q: %w", h.configPath, err)
	}

	return nil
}

// isDirListed reports whether config already says something about dir. It parses rather
// than searching for the table header, since the same directory can be spelled several
// ways in TOML and appending a table that is already there would leave the file
// refusing to parse at all.
func isDirListed(config []byte, dir string) (bool, error) {
	var parsed struct {
		Projects map[string]any `toml:"projects"`
	}
	if err := toml.Unmarshal(config, &parsed); err != nil {
		return false, fmt.Errorf("refusing to touch a codex config that does not parse: %w", err)
	}

	_, listed := parsed.Projects[dir]

	return listed, nil
}
