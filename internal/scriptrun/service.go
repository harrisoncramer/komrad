package scriptrun

import (
	"sync"

	"komrad/config"

	agentstore "komrad/internal/store/agent"
	runstore "komrad/internal/store/run"
	workflowstore "komrad/internal/store/workflow"
)

const (
	triggerProvision   = config.TriggerProvision
	triggerResume      = config.TriggerResume
	triggerDeprovision = config.TriggerDeprovision
)

type tmuxReadWriter interface {
	GetPaneOwner(pane string) string
	GetHostSocket() string
	GetInfoPane() string
	GetDefaultShell() string
}

type NewScriptServiceParams struct {
	Agents     *agentstore.AgentStore
	Workflows  *workflowstore.WorkflowStore
	Tmux       tmuxReadWriter
	Runner     scriptRunner
	Runs       *runstore.RunStore
	Load       configLoader
	Addr       string
	Socket     string
	BuiltinDir string
	LogsDir    string
	Editor     string
	BinPath    string
}

type ScriptService struct {
	agents     *agentstore.AgentStore
	workflows  *workflowstore.WorkflowStore
	tmux       tmuxReadWriter
	runner     scriptRunner
	runs       *runstore.RunStore
	load       configLoader
	addr       string
	socket     string
	builtinDir string
	logsDir    string
	editor     string
	binPath    string
	inFlightMu sync.Mutex
	inFlight   map[scriptKey]struct{}
	logRunMu   sync.Mutex
	HookQueue
}

// NewScriptService builds the service from its dependencies.
func NewScriptService(params NewScriptServiceParams) *ScriptService {
	return &ScriptService{
		agents:     params.Agents,
		workflows:  params.Workflows,
		tmux:       params.Tmux,
		runner:     params.Runner,
		runs:       params.Runs,
		load:       params.Load,
		addr:       params.Addr,
		socket:     params.Socket,
		builtinDir: params.BuiltinDir,
		logsDir:    params.LogsDir,
		editor:     params.Editor,
		binPath:    params.BinPath,
		inFlight:   make(map[scriptKey]struct{}),
	}
}

func isPaneOwnedBy(owner, agentID string) bool {
	return agentID != "" && owner == agentID
}
