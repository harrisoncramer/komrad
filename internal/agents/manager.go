// Package agents orchestrates the store and tmux to implement the panel actions
// and the CLI subcommands.
package agents

import (
	"context"
	"os"
	"path/filepath"

	"claude-agents/internal/store"
)

type tmuxClient interface {
	SetGlobal(name, value string)
	ShowGlobal(name string) string
	SetSessionOption(target, name, value string)
	DetachSession(name string)
	ClientPath(client string) string
	SendKeys(target string, keys ...string) error
	PaneAlive(pane string) bool
	CapturePane(pane string) string
	CurrentPane() (string, error)
	SplitRight(anchorPane, dir string) string
	NewWindow(session, name, dir string) (string, error)
	SwapPanes(src, dst string)
	RespawnPane(pane, dir string, argv ...string)
	KillPane(pane string)
}

// agentStore is the part of the store the manager reads and mutates. It is an
// interface so the manager's logic stays testable against a real store.
type agentStore interface {
	List(ctx context.Context) ([]store.Agent, error)
	ListAll(ctx context.Context) ([]store.Agent, error)
	Get(ctx context.Context, name string) (*store.Agent, error)
	GetTranscript(ctx context.Context, sessionID string) (store.Transcript, error)
	UpsertTranscript(ctx context.Context, sessionID, content string, offset int64) error
	Upsert(ctx context.Context, agent store.Agent) error
	SetStateByID(ctx context.Context, id, state string) error
	Rename(ctx context.Context, id, name string) (bool, error)
	Delete(ctx context.Context, name string) (bool, error)
	SoftDelete(ctx context.Context, name string) error
	Undelete(ctx context.Context, id string) error
	ActiveNameExists(ctx context.Context, name string) (bool, error)
	MoveToWorkflow(ctx context.Context, id string, workflowID *string) error
	ListWorkflows(ctx context.Context) ([]store.Workflow, error)
	GetWorkflow(ctx context.Context, id string) (*store.Workflow, error)
	CreateWorkflow(ctx context.Context, cfg store.WorkflowConfig) (*store.Workflow, error)
	RenameWorkflow(ctx context.Context, id, name string) error
	UpdateWorkflow(ctx context.Context, id string, cfg store.WorkflowConfig) error
	DeleteWorkflow(ctx context.Context, id string) error
	DeleteWorkflowWithChats(ctx context.Context, id string) error
}

// AgentManager lists, launches, and acts on Claude agents by coordinating a
// tmux client and the SQLite-backed store.
type AgentManager struct {
	tmux        tmuxClient
	sessions    agentStore
	selfPath    string
	claudeFlags string
	// serverAddr is where serve listens, which is also where the MCP endpoint
	// lives. Agents are told about it at launch.
	serverAddr string
}

// Params carries the AgentManager's dependencies.
type Params struct {
	Tmux        tmuxClient
	Store       agentStore
	SelfPath    string
	ClaudeFlags string
	ServerAddr  string
}

// defaultServerAddr matches the config package's default listen address, used
// when a caller does not pass one so a launched agent is still told a usable
// endpoint rather than a malformed URL.
const defaultServerAddr = "127.0.0.1:8787"

// NewAgentManager constructs an AgentManager from its dependencies.
func NewAgentManager(p Params) *AgentManager {
	addr := p.ServerAddr
	if addr == "" {
		addr = defaultServerAddr
	}

	return &AgentManager{
		tmux:        p.Tmux,
		sessions:    p.Store,
		selfPath:    p.SelfPath,
		claudeFlags: p.ClaudeFlags,
		serverAddr:  addr,
	}
}

// ManagerSession is the persistent tmux session the panel runs in. The popup
// attaches to it rather than launching the panel fresh, so hiding the popup
// leaves the panel running and reopening restores it. The tmux plugin uses the
// same literal.
const ManagerSession = "claude-manager"

func (m *AgentManager) PaneAlive(pane string) bool {
	return m.tmux.PaneAlive(pane)
}

// Reconciler builds the level-triggered reconciler over this manager's tmux
// client and store, for the serve process to run on a timer.
func (m *AgentManager) Reconciler() *Reconciler {
	return NewReconciler(m.tmux, m.sessions)
}

// TranscriptSyncer builds the transcript syncer over this manager's store,
// reading Claude's logs from ~/.claude/projects, for the serve process to run on
// a timer. An unresolvable home falls back to a relative path, which matches no
// logs rather than erroring.
func (m *AgentManager) TranscriptSyncer() *TranscriptSyncer {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	return NewTranscriptSyncer(m.sessions, filepath.Join(home, ".claude", "projects"))
}
