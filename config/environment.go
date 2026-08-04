package config

import (
	"os"
	"path/filepath"
	"strings"
)

const layerVariable = "WEAVE_CONFIG"

// Environment is the process context weave receives from its shell, tmux, or a launched
// agent. Its connection fields are copied from the values Viper already resolved; this
// package does not apply a second configuration precedence scheme.
type Environment struct {
	DatabaseURL         string
	ReadOnlyDatabaseURL string
	Addr                string
	TmuxSocket          string
	CacheDir            string
	Tmux                string
	TmuxPane            string
	AgentID             string
	SessionID           string
	Editor              string
}

// LayerPaths returns the config stack named by WEAVE_CONFIG using the platform's path-list
// separator, the same convention used by PATH and KUBECONFIG. Empty elements are dropped
// rather than handed on: PATH and KUBECONFIG both tolerate them, and the ordinary shell idiom
// for adding to an unset variable, `$WEAVE_CONFIG:$new`, produces a leading one on every
// machine that has not already exported the variable.
func LayerPaths() []string {
	value := os.Getenv(layerVariable)
	if value == "" {
		return nil
	}

	split := filepath.SplitList(value)
	paths := make([]string, 0, len(split))
	for _, path := range split {
		if strings.TrimSpace(path) != "" {
			paths = append(paths, path)
		}
	}

	return paths
}

// LoadEnvironment adds transient process context to connection values Viper has already resolved.
func LoadEnvironment(connection Environment) *Environment {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	connection.Tmux = os.Getenv("TMUX")
	connection.TmuxPane = os.Getenv("TMUX_PANE")
	connection.AgentID = os.Getenv("WEAVE_AGENT_ID")
	connection.SessionID = os.Getenv("WEAVE_SESSION_ID")
	connection.Editor = editor

	return &connection
}

// LookupEnv reports an environment value and whether it is non-empty. Configuration
// packages use this instead of reading the process environment outside this package.
func LookupEnv(name string) (string, bool) {
	value, set := os.LookupEnv(name)

	return value, set && value != ""
}

// LogPath is the file the background service writes to.
func (e *Environment) LogPath() string {
	return filepath.Join(e.CacheDir, "weave.log")
}
