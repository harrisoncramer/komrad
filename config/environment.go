package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"komrad/lib/protoenum"
	"komrad/lib/xslice"
	eventv1 "komrad/proto/gen/event/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"github.com/google/uuid"
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
	// reviews and forge credentials.
	StateDir              string
	EmbeddedPostgres      bool
	OwnerEmbeddedPostgres bool
	URL                   string
	Tmux                  string
	TmuxPane              string
	AgentID               uuid.UUID
	SessionID             string
	Editor                string
	Shell                 string
	Term                  string
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

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "sh"
	}

	connection.Tmux = os.Getenv("TMUX")
	connection.TmuxPane = os.Getenv("TMUX_PANE")
	if agentID := os.Getenv("KOMRAD_AGENT_ID"); agentID != "" {
		parsed, err := uuid.Parse(agentID)
		if err != nil {
			return nil, fmt.Errorf("failed to read KOMRAD_AGENT_ID %q: %w", agentID, err)
		}
		connection.AgentID = parsed
	}
	connection.SessionID = os.Getenv("KOMRAD_SESSION_ID")
	connection.Editor = editor
	connection.Shell = shell
	connection.Term = os.Getenv("TERM")

	return &connection, nil
}

// LookupEnv reports an environment value and whether it is non-empty.
func LookupEnv(name string) (string, bool) {
	value, set := os.LookupEnv(name)

	return value, set && value != ""
}

// LogPath is the file the server writes to.
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

// HookMoments are the moments a workflow can hang a script on, an agent's in the order they fire and then its task's.
func HookMoments() []eventv1.Moment {
	return protoenum.Accepted[eventv1.Moment]((&workflowv1.WorkflowHook{}).ProtoReflect().Descriptor().Fields().ByName("event"))
}

// MaxRunLogs is how many runs an agent keeps. Komrad prunes older ones as it writes new ones.
const MaxRunLogs = 50

// RunLogDir is where one agent's run logs live under dir.
func RunLogDir(dir string, agentID uuid.UUID) string {
	return filepath.Join(dir, agentID.String())
}

// RunLog is one run of a script, as the name of the file it wrote records it.
type RunLog struct {
	Number  int
	Trigger eventv1.Moment
	Script  string
	Path    string
}

// RunLogName is the file one run writes to. The number lets a listing sort the runs newest first
// without reading any of them, and the name says what set the run off.
func RunLogName(number int, trigger eventv1.Moment, script string) string {
	return fmt.Sprintf("%03d-%s-%s.log", number, protoenum.Name(trigger), script)
}

// ListRunLogs reads the runs an agent has recorded, newest first, parsing the number rather than
// sorting on it.
func ListRunLogs(dir string, agentID uuid.UUID) []RunLog {
	if dir == "" || agentID == uuid.Nil {
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
	for _, moment := range protoenum.Values[eventv1.Moment]() {
		if script, found := strings.CutPrefix(rest, protoenum.Name(moment)+"-"); found {
			return &RunLog{
				Number:  parsed,
				Trigger: moment,
				Script:  script,
			}, true
		}
	}

	return nil, false
}
