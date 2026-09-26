package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testCheckout(t *testing.T, config string) string {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "komrad.test.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	return root
}

func runSlot(t *testing.T, reg registry, env map[string]string, args ...string) (string, error) {
	t.Helper()

	var out bytes.Buffer
	w := world{
		getenv: func(name string) string { return env[name] },
		log:    reg.log,
		cmds:   reg.cmds,
	}
	err := w.run(t.Context(), append(args, "--registry", reg.dir), &out)

	return out.String(), err
}

func parseEnv(out string) map[string]string {
	env := map[string]string{}
	for line := range strings.Lines(out) {
		name, value, _ := strings.Cut(strings.TrimSuffix(line, "\n"), "=")
		env[name] = value
	}

	return env
}

// TestEnvThenCheck passes the environment env prints and refuses one leaking another instance.
func TestEnvThenCheck(t *testing.T) {
	t.Parallel()

	reg, _ := newTestRegistry(t)
	root := testCheckout(t, "# the slot adds [worktrees]\n[log]\nlevel = \"debug\"\n")

	out, err := runSlot(t, reg, nil, "env", "--root", root)
	if err != nil {
		t.Fatalf("env: %v", err)
	}

	env := parseEnv(out)
	if env["KOMRAD_ADDR"] != "127.0.0.1:8801" || env["DB_PORT"] != "5470" || env["KOMRAD_TEST_DATABASE"] != "komrad_1" || env["KOMRAD_TMUX_SOCKET"] != "komrad-test-1" {
		t.Errorf("env printed %q", out)
	}

	config, err := os.ReadFile(env["KOMRAD_CONFIG"])
	if err != nil {
		t.Fatal(err)
	}

	want := "# the slot adds [worktrees]\n[log]\nlevel = \"debug\"\n\n[worktrees]\ndir = \"" + filepath.Join(reg.slotDir(1), "worktrees") + "\"\n"
	if string(config) != want {
		t.Errorf("slot config = %q, want %q", config, want)
	}

	if _, err := runSlot(t, reg, env, "check", "--root", root); err != nil {
		t.Errorf("check of the environment env printed: %v", err)
	}

	leaked := parseEnv(out)
	leaked["KOMRAD_ADDR"] = "127.0.0.1:8787"
	if _, err := runSlot(t, reg, leaked, "check", "--root", root); err == nil || !strings.Contains(err.Error(), "KOMRAD_ADDR") {
		t.Errorf("check of a leaked address = %v, want a refusal naming KOMRAD_ADDR", err)
	}
}

// TestCheckRefusesACheckoutWithNoSlot refuses before anything is claimed.
func TestCheckRefusesACheckoutWithNoSlot(t *testing.T) {
	t.Parallel()

	reg, _ := newTestRegistry(t)
	root := testCheckout(t, "")

	if _, err := runSlot(t, reg, nil, "check", "--root", root); err == nil || !strings.Contains(err.Error(), "holds no slot") {
		t.Errorf("check = %v, want a refusal", err)
	}
}

// TestEnvRefusesATestConfigNamingWorktrees keeps the slot the owner of its worktrees table.
func TestEnvRefusesATestConfigNamingWorktrees(t *testing.T) {
	t.Parallel()

	reg, _ := newTestRegistry(t)
	root := testCheckout(t, "[worktrees]\ndir = \"/elsewhere\"\n")

	if _, err := runSlot(t, reg, nil, "env", "--root", root); err == nil || !strings.Contains(err.Error(), "[worktrees]") {
		t.Errorf("env = %v, want a refusal", err)
	}
}

// TestDownGivesTheSlotBack frees the slot and treats a second down as nothing to do.
func TestDownGivesTheSlotBack(t *testing.T) {
	t.Parallel()

	reg, _ := newTestRegistry(t)
	root := testCheckout(t, "")

	if _, err := runSlot(t, reg, nil, "env", "--root", root); err != nil {
		t.Fatalf("env: %v", err)
	}

	if _, err := runSlot(t, reg, nil, "down", "--root", root); err != nil {
		t.Fatalf("down: %v", err)
	}

	if _, err := os.Stat(reg.slotDir(1)); err == nil {
		t.Error("slot 1 is still held after down")
	}

	if _, err := runSlot(t, reg, nil, "down", "--root", root); err != nil {
		t.Errorf("down of a checkout with no slot: %v", err)
	}
}
