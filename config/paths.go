package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Worktrees is where a workflow's generated git worktree provisioner puts what it creates.
type Worktrees struct {
	// Directory that holds generated worktrees, one subdirectory per agent.
	// Left empty this is ~/.local/state/komrad/worktrees.
	Dir string `mapstructure:"dir"`
	// Whether komrad runs `mise install` in each worktree it sets up or resumes, so the tools the
	// repository pins are there before the agent starts. mise refuses a config it does not trust,
	// so the worktrees directory has to be in mise's trusted_config_paths.
	MiseInstall bool `mapstructure:"mise_install"`
}

// Context is the standing text komrad starts every agent with, above whatever its workflow
// adds. It is where what is true of your machine and how you want to be worked with goes,
// so it does not have to be pasted into every workflow.
type Context struct {
	// The file holding global context, read at each launch.
	// Left empty this is ~/.config/komrad/context.md, but will not trigger a failure if left blank.
	File string `mapstructure:"file"`
}

// configHome is the directory the panel keeps its own files in, ~/.config/komrad.
func configHome() (string, error) {
	return underHome(".config", "komrad")
}

func underHome(elem ...string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	return filepath.Join(append([]string{home}, elem...)...), nil
}

// ExpandHome expands a leading ~ or ~/ to the user's home directory, since nothing below a
// config does it for itself. tmux's new-session -c takes the path literally, and so does
// every open of a file komrad was pointed at. A home directory that cannot be read leaves
// the path as written.
func ExpandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}

	return filepath.Join(home, path[len("~/"):])
}

// GetHooksDir is where komrad looks for the scripts a workflow hangs on a moment: the
// configured directory, or ~/.config/komrad/hooks when the config names none.
func (c Config) GetHooksDir() (string, error) {
	if dir := strings.TrimSpace(c.Hooks.Dir); dir != "" {
		return ExpandHome(dir), nil
	}

	home, err := configHome()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, "hooks"), nil
}

// GetWorktreesDir is where a generated worktree provisioner creates its worktrees: the
// configured directory, or worktrees under the state directory when the config names none.
func (c Config) GetWorktreesDir() (string, error) {
	if dir := strings.TrimSpace(c.Worktrees.Dir); dir != "" {
		return ExpandHome(dir), nil
	}

	stateDir := c.Connection.StateDir
	if stateDir == "" {
		dir, err := getDefaultStateDir()
		if err != nil {
			return "", err
		}

		stateDir = dir
	}

	return filepath.Join(stateDir, "worktrees"), nil
}

// getDefaultStateDir returns ~/.local/state/komrad, the state directory used when the config names none.
func getDefaultStateDir() (string, error) {
	return underHome(".local", "state", "komrad")
}

// GetContextFile is the file of standing text komrad starts every agent with: the configured file, or
// ~/.config/komrad/context.md when the config names none. The default is filled in here for
// the reason the hooks directory's is, since one machine's home directory spelled into
// the generated config would be wrong on every other machine.
func (c Config) GetContextFile() (string, error) {
	if file := strings.TrimSpace(c.Context.File); file != "" {
		return ExpandHome(file), nil
	}

	home, err := configHome()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, "context.md"), nil
}
