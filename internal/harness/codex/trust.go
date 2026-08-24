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

	grown := string(current)
	if grown != "" {
		grown += "\n"
	}
	grown += fmt.Sprintf("[projects.%s]\ntrust_level = %s\n", tomlString(dir), tomlString("trusted"))

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
