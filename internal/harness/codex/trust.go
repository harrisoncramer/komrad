package codex

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// resolveConfigPath names the file Codex keeps its trusted directories in.
func (h *Harness) resolveConfigPath() (string, error) {
	home, err := h.resolveHome()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, "config.toml"), nil
}

func (h *Harness) trustDir(dir string) error {
	if !h.trustLaunchDirs || dir == "" {
		return nil
	}

	configPath, err := h.resolveConfigPath()
	if err != nil {
		return err
	}

	current, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read the codex config %q: %w", configPath, err)
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
		return fmt.Errorf("failed to parse the codex config as it would be written: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return fmt.Errorf("failed to create the codex config dir: %w", err)
	}

	if err := os.WriteFile(configPath, []byte(grown), 0o644); err != nil {
		return fmt.Errorf("failed to write the codex config %q: %w", configPath, err)
	}

	return nil
}

func isDirListed(config []byte, dir string) (bool, error) {
	var parsed struct {
		Projects map[string]any `toml:"projects"`
	}
	if err := toml.Unmarshal(config, &parsed); err != nil {
		return false, fmt.Errorf("failed to parse the codex config: %w", err)
	}

	_, listed := parsed.Projects[dir]

	return listed, nil
}
