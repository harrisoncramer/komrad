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

const serverMaxConns = 4

// dexIssuer is the identity provider the test docker compose runs, which every slot signs in with.
const dexIssuer = "http://127.0.0.1:5556/dex"

// dexClientID is the id komrad is registered under in that provider.
const dexClientID = "komrad"

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
		{"KOMRAD_DATABASE_URL", fmt.Sprintf("postgres://%s:komrad@localhost:%d/%s?sslmode=disable&pool_max_conns=%d", getLogin(n), containerPort, database, serverMaxConns)},
		{"KOMRAD_OWNER_DATABASE_URL", fmt.Sprintf("postgres://komrad:komrad@localhost:%d/%s?sslmode=disable", containerPort, database)},
		{"KOMRAD_READONLY_DATABASE_URL", ""},
		{"KOMRAD_ADDR", serverAddr(n)},
		{"KOMRAD_TMUX_SOCKET", getSocket(n)},
		{"COMPOSE_PROJECT_NAME", containerProject},
		{"KOMRAD_CACHE_DIR", filepath.Join(slotDir, "cache")},
		{"KOMRAD_STATE_DIR", filepath.Join(slotDir, "state")},
		{"KOMRAD_CONFIG", configPath(slotDir)},
		{"KOMRAD_ENGINES_DIR", enginesDir(slotDir)},
	}
}

func serverAddr(n int) string {
	return fmt.Sprintf("127.0.0.1:%d", 8800+n)
}

// signInAddr is where slot n's server takes signed-in requests.
func signInAddr(n int) string {
	return fmt.Sprintf("127.0.0.1:%d", 8900+n)
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

func enginesDir(slotDir string) string {
	return filepath.Join(slotDir, "engines")
}

// writeConfig writes the slot's config.
func writeConfig(root, slotDir string, n int) error {
	base, err := os.ReadFile(filepath.Join(root, "komrad.test.toml"))
	if err != nil {
		return fmt.Errorf("failed to read the test config: %w", err)
	}

	for line := range strings.Lines(string(base)) {
		switch table := strings.TrimSpace(line); table {
		case "[worktrees]", "[context]", "[hooks]", "[sign_in]":
			return fmt.Errorf("komrad.test.toml names a %s table, which the slot owns", table)
		}
	}

	worktrees := filepath.Join(slotDir, "worktrees")
	context := filepath.Join(slotDir, "context.md")
	hooks := filepath.Join(slotDir, "hooks")
	content := fmt.Sprintf("%s\n[worktrees]\ndir = %q\n\n[context]\nfile = %q\n\n[hooks]\ndir = %q\n\n[sign_in]\nissuer = %q\nclient_id = %q\naddr = %q\n",
		base, worktrees, context, hooks, dexIssuer, dexClientID, signInAddr(n))

	return writeIfChanged(configPath(slotDir), content)
}

// writeEngine writes the engine file that reaches slot n's server at its signed-in listener, so
// `komrad --engine` with the slot's engine name signs in the way a remote engine does.
func writeEngine(slotDir string, n int) error {
	dir := enginesDir(slotDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to make the slot's engines directory: %w", err)
	}

	content := fmt.Sprintf("[connection]\nurl = %q\n", "http://"+signInAddr(n))

	return writeIfChanged(filepath.Join(dir, getEngine(n)+".toml"), content)
}

// writeIfChanged replaces the file at path with content through a rename, unless it holds it already.
func writeIfChanged(path, content string) error {
	existing, err := os.ReadFile(path)
	if err == nil && string(existing) == content {
		return nil
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", path, err)
	}

	_, err = tmp.WriteString(content)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}

	if err == nil {
		err = os.Rename(tmp.Name(), path) //nolint:forbidigo // a module of its own, which cannot import lib/xfile
	}

	if err != nil {
		if removeErr := os.Remove(tmp.Name()); removeErr != nil {
			return fmt.Errorf("failed to write %s: %w, and failed to remove %s: %w", path, err, tmp.Name(), removeErr)
		}

		return fmt.Errorf("failed to write %s: %w", path, err)
	}

	return nil
}
