package agents

import (
	"context"
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
	IsPaneAlive(pane string) bool
	RespawnPane(pane, dir string, argv ...string)
	WaitFor(ctx context.Context, channel string) error
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

type ScriptParams struct {
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
func NewScriptService(p ScriptParams) *ScriptService {
	return &ScriptService{
		agents:     p.Agents,
		workflows:  p.Workflows,
		tmux:       p.Tmux,
		runner:     p.Runner,
		runs:       p.Runs,
		load:       p.Load,
		addr:       p.Addr,
		socket:     p.Socket,
		builtinDir: p.BuiltinDir,
		logsDir:    p.LogsDir,
		editor:     p.Editor,
		binPath:    p.BinPath,
		inFlight:   make(map[scriptKey]struct{}),
	}
}
