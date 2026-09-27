package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// slotMax is how many throwaway instances may exist at once.
const slotMax = 50

// containerProject is the compose project of the Postgres every slot keeps its database in.
const containerProject = "komrad-test-pg"

// containerPort is where that Postgres listens on the host.
const containerPort = 5470

// setting is one environment variable a slot derives.
type setting struct {
	name  string
	value string
}

func buildSettings(slotDir string, n int) []setting {
	database := getDatabase(n)

	return []setting{
		{"DB_PORT", fmt.Sprint(containerPort)},
		{"KOMRAD_TEST_DATABASE", database},
		{"KOMRAD_TEST_ENGINE", getEngine(n)},
		{"KOMRAD_DATABASE_URL", fmt.Sprintf("postgres://%s:komrad@localhost:%d/%s?sslmode=disable", getLogin(n), containerPort, database)},
		{"KOMRAD_OWNER_DATABASE_URL", fmt.Sprintf("postgres://komrad:komrad@localhost:%d/%s?sslmode=disable", containerPort, database)},
		{"KOMRAD_READONLY_DATABASE_URL", ""},
		{"KOMRAD_ADDR", serverAddr(n)},
		{"KOMRAD_TMUX_SOCKET", getSocket(n)},
		{"COMPOSE_PROJECT_NAME", containerProject},
		{"KOMRAD_CACHE_DIR", filepath.Join(slotDir, "cache")},
		{"KOMRAD_CONFIG", configPath(slotDir)},
	}
}

func serverAddr(n int) string {
	return fmt.Sprintf("127.0.0.1:%d", 8800+n)
}

func getSocket(n int) string {
	return fmt.Sprintf("komrad-test-%d", n)
}

func getDatabase(n int) string {
	return fmt.Sprintf("komrad_%d", n)
}

// getEngine is the engine slot n's server runs as.
func getEngine(n int) string {
	return fmt.Sprintf("slot_%d", n)
}

func getLogin(n int) string {
	return "komrad_engine_" + getEngine(n)
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
		err = os.Rename(tmp.Name(), configPath(slotDir))
	}

	if err != nil {
		if removeErr := os.Remove(tmp.Name()); removeErr != nil {
			return fmt.Errorf("failed to write the slot config: %w, and failed to remove %s: %w", err, tmp.Name(), removeErr)
		}

		return fmt.Errorf("failed to write the slot config: %w", err)
	}

	return nil
}
