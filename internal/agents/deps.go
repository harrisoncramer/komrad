package agents

import (
	"context"

	"komrad/internal/harness"
	"komrad/internal/scriptrun"
	"komrad/internal/scripts"
	"komrad/internal/store"
	"komrad/internal/tmux"

	agentstore "komrad/internal/store/agent"
)

var (
	ErrBadDirectory         = scripts.ErrBadDirectory
	ErrHarnessUnknown       = harness.ErrNotRegistered
	ErrScriptNotFound       = scripts.ErrScriptNotFound
	ErrScriptNotExecutable  = scripts.ErrNotExecutable
	ErrScriptNotStartable   = scripts.ErrNotStartable
	ErrScriptDirMissing     = scripts.ErrDirMissing
	ErrProvisionFailed      = scriptrun.ErrProvisionFailed
	ErrHookFailed           = scriptrun.ErrHookFailed
	ErrScriptAlreadyRunning = scriptrun.ErrScriptAlreadyRunning
)

type (
	Script       = scripts.Script
	ScriptHeader = scripts.Header
	Report       = harness.Report
	ReportEvent  = harness.ReportEvent
)

const (
	ReportTurnEnded      = harness.ReportTurnEnded
	ReportTurnFailed     = harness.ReportTurnFailed
	ReportToolStarted    = harness.ReportToolStarted
	ReportToolFinished   = harness.ReportToolFinished
	ReportApprovalWanted = harness.ReportApprovalWanted
	ReportCompacted      = harness.ReportCompacted
)

type paneLister interface {
	GetPanes() (map[string]tmux.PaneStatus, error)
}

type paneKiller interface {
	paneLister
	KillPane(pane string)
}

// paneOwnerReader asks tmux who owns a pane, one at a time or in a listing.
type paneOwnerReader interface {
	paneLister
	GetPaneOwner(pane string) string
}

// paneExitReader asks tmux whether the CLI launched in a pane has exited, and what it left on screen.
type paneExitReader interface {
	HasCLIExited(pane string) bool
	CapturePane(pane string) string
}

type paneController interface {
	paneKiller
	paneOwnerReader
	paneExitReader
	NewWindow(session, name, dir string) (string, error)
	SetPaneOwner(pane, agentID string) error
	GetHostPath() string
}

// paneStarter runs an agent's CLI as the pane's own process.
type paneStarter interface {
	RespawnPane(pane, dir string, argv ...string) error
	GetDefaultShell() string
}

type txRunner interface {
	InTx(ctx context.Context, fn func(store.DBTX) error) error
}

type harnessGetter interface {
	GetByID(id string) (harness.Harness, error)
}

// buildTarget names an agent the way a channel or an observer reaches it.
func buildTarget(agent agentstore.Agent) harness.Target {
	return harness.Target{
		AgentID:   agent.ID,
		Pane:      agent.Pane,
		HarnessID: agent.Harness,
	}
}

// agentScriptRunner is the seam onto the executables a user hangs off an agent's life.
type agentScriptRunner interface {
	scriptrun.HookRunner
	RunProvision(ctx context.Context, agent agentstore.Agent, name string) (string, error)
	RunResume(ctx context.Context, agent agentstore.Agent, name string) (string, error)
	RunDeprovision(ctx context.Context, agent agentstore.Agent, name string, done func(error)) error
}
