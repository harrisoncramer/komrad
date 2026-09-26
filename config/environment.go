package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"komrad/lib/xslice"
)

const layerVariable = "KOMRAD_CONFIG"

// Environment is the process context komrad receives from its shell, tmux, or a launched
// agent, with its connection fields copied from what Viper already resolved.
type Environment struct {
	DatabaseURL         string
	ReadOnlyDatabaseURL string
	OwnerDatabaseURL    string
	Addr                string
	TmuxSocket          string
	CacheDir            string
	// StateDir is where komrad keeps its own files about agents, such as worktrees, released
	// reviews and forge credentials, and is the user's state directory unless a test says
	// otherwise.
	StateDir         string
	SSH              string
	EngineTmuxSocket string
	Exec             []string
	Forward          []string
	EngineUser       string
	Tmux             string
	TmuxPane         string
	AgentID          string
	SessionID        string
	Editor           string
}

// LayerPaths returns the config stack named by KOMRAD_CONFIG using the platform's path-list separator, the same convention used by PATH.
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
func LoadEnvironment(connection Environment) (*Environment, error) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	if connection.StateDir == "" {
		stateDir, err := GetStateDir()
		if err != nil {
			return nil, err
		}

		connection.StateDir = stateDir
	}

	connection.Tmux = os.Getenv("TMUX")
	connection.TmuxPane = os.Getenv("TMUX_PANE")
	connection.AgentID = os.Getenv("KOMRAD_AGENT_ID")
	connection.SessionID = os.Getenv("KOMRAD_SESSION_ID")
	connection.Editor = editor

	return &connection, nil
}

// LookupEnv reports an environment value and whether it is non-empty.
func LookupEnv(name string) (string, bool) {
	value, set := os.LookupEnv(name)

	return value, set && value != ""
}

// LogPath is the file the background service writes to.
func (e *Environment) LogPath() string {
	return filepath.Join(e.CacheDir, "komrad.log")
}

// RunLogsDir is where a script's own output is kept, one directory per agent and one file per
// run inside it, so it is still there to read after the pane it ran in has closed.
func (e *Environment) RunLogsDir() string {
	return filepath.Join(e.CacheDir, "komrad-run-logs")
}

// FramesDir is where the pane captures are kept when debug mode is on, one directory per agent.
func (e *Environment) FramesDir() string {
	return filepath.Join(e.CacheDir, "komrad-frames")
}

// The moments a script can run at, which are what a script reads out of KOMRAD_TRIGGER and
// what a run's log file is named after: the worktree provisioner's own three, and the moments
// a workflow can hang a hook on.
const (
	TriggerProvision   = "provision"
	TriggerResume      = "resume"
	TriggerDeprovision = "deprovision"
	HookPreLaunch      = "pre-launch"
	HookPostLaunch     = "post-launch"
	HookPostResume     = "post-resume"
	HookPostRestore    = "post-restore"
	HookPreTeardown    = "pre-teardown"
)

// runMoments is every moment by name.
func runMoments() []string {
	return []string{
		TriggerProvision,
		TriggerResume,
		TriggerDeprovision,
		HookPreLaunch,
		HookPostLaunch,
		HookPostResume,
		HookPostRestore,
		HookPreTeardown,
	}
}

// MaxRunLogs is how many runs an agent keeps, older ones being pruned as new ones are written.
const MaxRunLogs = 50

// RunLogDir is where one agent's run logs live under dir.
func RunLogDir(dir, agentID string) string {
	return filepath.Join(dir, agentID)
}

// RunLog is one run of a script, as the name of the file it wrote records it.
type RunLog struct {
	Number  int
	Trigger string
	Script  string
	Path    string
}

// RunLogName is the file one run writes to, numbered so the runs can be listed newest first
// without reading any of them and named so a run says what set it off.
func RunLogName(number int, trigger, script string) string {
	return fmt.Sprintf("%03d-%s-%s.log", number, trigger, script)
}

// ListRunLogs reads the runs an agent has recorded, newest first, parsing the number rather than
// sorting on it.
func ListRunLogs(dir, agentID string) []RunLog {
	if dir == "" || agentID == "" {
		return nil
	}

	logDir := RunLogDir(dir, agentID)
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return nil
	}

	runs := make([]RunLog, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		run, ok := parseRunLogName(entry.Name())
		if !ok {
			continue
		}
		run.Path = filepath.Join(logDir, entry.Name())
		runs = append(runs, *run)
	}

	slices.SortFunc(runs, func(a, b RunLog) int {
		return b.Number - a.Number
	})

	return runs
}

func parseRunLogName(name string) (*RunLog, bool) {
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

	// A moment's name holds a hyphen of its own, so the split is against the moments there are
	// rather than at the next hyphen along.
	for _, moment := range runMoments() {
		if script, found := strings.CutPrefix(rest, moment+"-"); found {
			return &RunLog{
				Number:  parsed,
				Trigger: moment,
				Script:  script,
			}, true
		}
	}

	trigger, script, ok := strings.Cut(rest, "-")
	if !ok {
		return nil, false
	}

	return &RunLog{
		Number:  parsed,
		Trigger: trigger,
		Script:  script,
	}, true
}
