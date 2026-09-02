package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"weave/lib/xslice"
)

const layerVariable = "WEAVE_CONFIG"

// Environment is the process context weave receives from its shell, tmux, or a launched
// agent, with its connection fields copied from what Viper already resolved.
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

// LayerPaths returns the config stack named by WEAVE_CONFIG using the platform's path-list separator, the same convention used by PATH.
func LayerPaths() []string {
	value := os.Getenv(layerVariable)
	if value == "" {
		return nil
	}

	// Only include valid paths.
	trimmed := xslice.Map(filepath.SplitList(value), strings.TrimSpace)
	return xslice.Filter(trimmed, func(path string) bool {
		return path != ""
	})
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

// LookupEnv reports an environment value and whether it is non-empty.
func LookupEnv(name string) (string, bool) {
	value, set := os.LookupEnv(name)

	return value, set && value != ""
}

// LogPath is the file the background service writes to.
func (e *Environment) LogPath() string {
	return filepath.Join(e.CacheDir, "weave.log")
}

// ActionLogsDir is where an action's own output is kept, one directory per agent and one file
// per run inside it, so it is still there to read after the pane it ran in has closed.
func (e *Environment) ActionLogsDir() string {
	return filepath.Join(e.CacheDir, "weave-action-logs")
}

// The triggers an action can run under, which are what a script reads out of WEAVE_TRIGGER
// and what a run's log file is named after.
const (
	TriggerManual      = "manual"
	TriggerProvision   = "provision"
	TriggerResume      = "resume"
	TriggerDeprovision = "deprovision"
)

// MaxActionLogRuns is how many runs an agent keeps, older ones being pruned as new ones are
// written, since an agent resumed daily for a year would otherwise hold a file for every one
// of those days.
const MaxActionLogRuns = 50

// ActionLogDir is where one agent's action logs live under dir.
func ActionLogDir(dir, agentID string) string {
	return filepath.Join(dir, agentID)
}

// ActionLogRun is one run of an action, as the name of the file it wrote records it.
type ActionLogRun struct {
	Number  int
	Trigger string
	Action  string
	Path    string
}

// ActionLogName is the file one run writes to, numbered so the runs can be listed newest
// first without reading any of them and named so a run says what set it off.
func ActionLogName(number int, trigger, action string) string {
	return fmt.Sprintf("%03d-%s-%s.log", number, trigger, action)
}

// ListActionLogRuns reads the runs an agent has recorded, newest first, parsing the number
// rather than sorting on it, since a run past the ninth hundred is wider than the ones before
// it and would otherwise sort under them.
func ListActionLogRuns(dir, agentID string) []ActionLogRun {
	if dir == "" || agentID == "" {
		return nil
	}

	logDir := ActionLogDir(dir, agentID)
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return nil
	}

	runs := make([]ActionLogRun, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		run, ok := parseActionLogName(entry.Name())
		if !ok {
			continue
		}
		run.Path = filepath.Join(logDir, entry.Name())
		runs = append(runs, *run)
	}

	slices.SortFunc(runs, func(a, b ActionLogRun) int {
		return b.Number - a.Number
	})

	return runs
}

func parseActionLogName(name string) (*ActionLogRun, bool) {
	trimmed, ok := strings.CutSuffix(name, ".log")
	if !ok {
		return nil, false
	}

	number, rest, ok := strings.Cut(trimmed, "-")
	if !ok {
		return nil, false
	}

	parsed, err := strconv.Atoi(number)
	if err != nil {
		return nil, false
	}

	trigger, action, ok := strings.Cut(rest, "-")
	if !ok {
		return nil, false
	}

	return &ActionLogRun{Number: parsed, Trigger: trigger, Action: action}, true
}
