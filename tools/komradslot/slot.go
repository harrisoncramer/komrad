package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// slotMax is how many throwaway instances may exist at once.
const slotMax = 50

// setting is one environment variable a slot derives.
type setting struct {
	name  string
	value string
}

func buildSettings(slotDir string, n int) []setting {
	return []setting{
		{"KOMRAD_DATABASE_URL", ""},
		{"KOMRAD_OWNER_DATABASE_URL", ""},
		{"KOMRAD_READONLY_DATABASE_URL", ""},
		{"KOMRAD_ADDR", serverAddr(n)},
		{"KOMRAD_TMUX_SOCKET", getSocket(n)},
		{"KOMRAD_CACHE_DIR", filepath.Join(slotDir, "cache")},
		{"KOMRAD_STATE_DIR", getStateDir(slotDir)},
		{"KOMRAD_ENGINES_DIR", filepath.Join(slotDir, "engines")},
		{"KOMRAD_CONFIG", configPath(slotDir)},
	}
}

func serverAddr(n int) string {
	return fmt.Sprintf("127.0.0.1:%d", 8800+n)
}

func getSocket(n int) string {
	return fmt.Sprintf("komrad-test-%d", n)
}

func getStateDir(slotDir string) string {
	return filepath.Join(slotDir, "state")
}

func getPostgresDir(slotDir string) string {
	return filepath.Join(getStateDir(slotDir), "postgres")
}

func configPath(slotDir string) string {
	return filepath.Join(slotDir, "komrad.toml")
}

// writeConfig writes the slot's config.
func writeConfig(root, slotDir string) error {
	base, err := os.ReadFile(filepath.Join(root, "komrad.test.toml"))
	if err != nil {
		return fmt.Errorf("failed to read the test config: %w", err)
	}

	for line := range strings.Lines(string(base)) {
		switch table := strings.TrimSpace(line); table {
		case "[worktrees]", "[context]", "[hooks]":
			return fmt.Errorf("komrad.test.toml names a %s table, which the slot owns", table)
		}
	}

	worktrees := filepath.Join(slotDir, "worktrees")
	context := filepath.Join(slotDir, "context.md")
	hooks := filepath.Join(slotDir, "hooks")
	content := fmt.Sprintf("%s\n[worktrees]\ndir = %q\n\n[context]\nfile = %q\n\n[hooks]\ndir = %q\n", base, worktrees, context, hooks)

	existing, err := os.ReadFile(configPath(slotDir))
	if err == nil && string(existing) == content {
		return nil
	}

	tmp, err := os.CreateTemp(slotDir, "komrad.toml.*")
	if err != nil {
		return fmt.Errorf("failed to create the slot config: %w", err)
	}

	_, err = tmp.WriteString(content)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}

	if err == nil {
		err = os.Rename(tmp.Name(), configPath(slotDir)) //nolint:forbidigo // a module of its own, which cannot import lib/xfile
	}

	if err != nil {
		if removeErr := os.Remove(tmp.Name()); removeErr != nil {
			return fmt.Errorf("failed to write the slot config: %w, and failed to remove %s: %w", err, tmp.Name(), removeErr)
		}

		return fmt.Errorf("failed to write the slot config: %w", err)
	}

	return nil
}
