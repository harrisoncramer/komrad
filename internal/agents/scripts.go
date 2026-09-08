package agents

import (
	"sync"

	"weave/config"
)

const (
	TriggerProvision   = config.TriggerProvision
	TriggerResume      = config.TriggerResume
	TriggerDeprovision = config.TriggerDeprovision
)

type tmuxReadWriter interface {
	GetPaneOwner(pane string) string
	GetHostSocket() string
	GetInfoPane() string
	GetDefaultShell() string
}

type ScriptService struct {
	agents     agentGetter
	workflows  workflowReader
	tmux       tmuxReadWriter
	runner     scriptRunner
	runs       runRecorder
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
}

type NewScriptServiceParams struct {
	Agents     agentGetter
	Workflows  workflowReader
	Tmux       tmuxReadWriter
	Runner     scriptRunner
	Runs       runRecorder
	Load       configLoader
	Addr       string
	Socket     string
	BuiltinDir string
	LogsDir    string
	Editor     string
	BinPath    string
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
