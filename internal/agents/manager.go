// Package agents orchestrates the store and tmux to implement the panel actions
// and the CLI subcommands.
package agents

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"claude-agents/internal/store"
	"claude-agents/lib/xstr"

	"github.com/google/uuid"
)

// expandHome expands a leading ~ or ~/ to the user's home. tmux's new-session -c
// does no shell tilde expansion, so a stored ~ path would fail to chdir.
// Expanding here keeps the cwd absolute.
func expandHome(dir string) string {
	if dir != "~" && !strings.HasPrefix(dir, "~/") {
		return dir
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return dir
	}
	if dir == "~" {
		return home
	}

	return filepath.Join(home, dir[len("~/"):])
}

// tmuxClient is the part of the tmux client the manager drives. It is an
// interface so tests can substitute a recording fake for the real CLI.
type tmuxClient interface {
	SetGlobal(name, value string)
	ShowGlobal(name string) string
	SetSessionOption(target, name, value string)
	DetachSession(name string)
	ClientPath(client string) string
	SendKeys(target string, keys ...string)
	PaneAlive(pane string) bool
	CapturePane(pane string) string
	CurrentPane() (string, error)
	SplitRight(anchorPane, dir string) string
	NewWindow(session, name, dir string) string
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

	// The stage is the pane beside the panel that shows the hovered resource.
	// navPane anchors the split. stagePane is the pane the panel drives, either bat
	// or a blank shell. stagedChatPane is the agent pane currently swapped into the
	// slot, empty when the stage shows stagePane itself. InitStage sets these up
	// once the panel knows its own pane.
	navPane        string
	stagePane      string
	stagedChatPane string
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

// managerOpenFlag marks that the popup is currently shown, so the binding hides
// it rather than opening a second one. The open command sets it while the popup
// is up and clears it on close. The tmux plugin reads it.
const managerOpenFlag = "@claude_manager_open"

// managerClientFlag holds the tmux client that opened the current popup. The open
// command writes it on each open, and the manager reads it live, so actions that
// need the invoking client use the one actually viewing the popup rather than one
// fixed when the persistent panel first started.
const managerClientFlag = "@claude_manager_client"

// MarkOpen records that the popup is shown.
func (m *AgentManager) MarkOpen() { m.tmux.SetGlobal(managerOpenFlag, "1") }

// MarkClosed records that the popup is hidden.
func (m *AgentManager) MarkClosed() { m.tmux.SetGlobal(managerOpenFlag, "0") }

// SetInvokingClient records the tmux client that opened the current popup.
func (m *AgentManager) SetInvokingClient(client string) {
	m.tmux.SetGlobal(managerClientFlag, client)
}

// HideManager hides the popup by detaching the client viewing the persistent
// manager session. The session stays alive, so the next open restores it.
func (m *AgentManager) HideManager() { m.tmux.DetachSession(ManagerSession) }

// SilenceStatus turns off a session's tmux status bar, so the manager panel and
// the agents it opens fill the popup without a tmux footer under them.
func (m *AgentManager) SilenceStatus(session string) {
	m.tmux.SetSessionOption(session, "status", "off")
}

// InvokingClient returns the tmux client that opened the current popup.
func (m *AgentManager) InvokingClient() string { return m.tmux.ShowGlobal(managerClientFlag) }

// PaneAlive reports whether an agent's tmux pane still exists, for pruning
// entries the panel lists from the service.
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

// InitStage records the panel's own pane as the anchor and splits a stage pane
// to its right, rooted at home. The panel calls it once on startup. It is a
// no-op split if a live stage already exists, so a panel restart does not stack
// splits.
func (m *AgentManager) InitStage() {
	if pane, err := m.tmux.CurrentPane(); err == nil && pane != "" {
		m.navPane = pane
	}
	m.ensureStage()
}

// ensureStage guarantees a live stage pane beside the panel, splitting a fresh
// one when the stage is missing or has died. A one-shot preview process like bat
// can exit and take its pane down with it. This self-heals without a panel
// restart. A stage pane swapped out to hold a chat is still alive, so it is left
// as is.
func (m *AgentManager) ensureStage() {
	if m.navPane == "" {
		return
	}
	if m.stagePane != "" && m.tmux.PaneAlive(m.stagePane) {
		return
	}

	home, _ := os.UserHomeDir()
	m.stagePane = m.tmux.SplitRight(m.navPane, home)
	m.stagedChatPane = ""
}

// restoreStage swaps any staged chat pane back to its own window, leaving the
// panel's own stagePane sitting in the slot again. A no-op when nothing is
// staged.
func (m *AgentManager) restoreStage() {
	if m.stagedChatPane == "" {
		return
	}

	m.tmux.SwapPanes(m.stagePane, m.stagedChatPane)
	m.stagedChatPane = ""
}

// StageChat swaps a live agent's pane into the stage slot, so its running claude
// shows beside the panel. Focus stays on the panel. Moving to the chat is a plain
// tmux pane move the user drives. A no-op when that chat is already staged.
func (m *AgentManager) StageChat(pane string) {
	m.ensureStage()
	if m.stagePane == "" || pane == "" || pane == m.stagedChatPane {
		return
	}

	m.restoreStage()
	m.tmux.SwapPanes(pane, m.stagePane)
	m.stagedChatPane = pane
}

// StageText shows markdown in the stage by writing it to a temp file and running
// bat there. Used for a workflow's info and for a dormant chat's placeholder. bat
// is one-shot, so it runs inside a shell that holds the pane open after it
// prints. Otherwise the process would exit and tmux would close the stage pane.
func (m *AgentManager) StageText(markdown string) {
	m.ensureStage()
	if m.stagePane == "" {
		return
	}

	m.restoreStage()

	home, _ := os.UserHomeDir()
	path := filepath.Join(os.TempDir(), "claude-agents-stage.md")
	if err := os.WriteFile(path, []byte(markdown), 0o644); err != nil {
		return
	}

	cmd := fmt.Sprintf("bat --language=markdown --style=plain --paging=never '%s'; while :; do sleep 3600; done", path)
	m.tmux.RespawnPane(m.stagePane, home, "sh", "-c", cmd)
}

// transcriptPreviewBefore and transcriptPreviewAfter bound how many lines show
// around a matched line in a search preview. Wide enough to read the match in its
// surrounding conversation, but still a window into a large transcript. bat
// renders from the top, so showing the whole thing would push the match
// off-screen.
const (
	transcriptPreviewBefore = 12
	transcriptPreviewAfter  = 80
)

// StageTranscript shows a transcript in the stage centered on a matched line,
// with that line highlighted, so a search hit reads in the context around it. A
// highlightLine of zero shows the transcript from the top. An empty transcript
// shows a placeholder.
func (m *AgentManager) StageTranscript(content string, highlightLine int) {
	if strings.TrimSpace(content) == "" {
		m.StageText("_no transcript yet_")
		return
	}

	m.ensureStage()
	if m.stagePane == "" {
		return
	}

	m.restoreStage()

	home, _ := os.UserHomeDir()
	path := filepath.Join(os.TempDir(), "claude-agents-stage.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return
	}

	args := "--language=markdown --style=plain --paging=never"
	if highlightLine > 0 {
		start := highlightLine - transcriptPreviewBefore
		if start < 1 {
			start = 1
		}
		args += fmt.Sprintf(" --line-range %d:%d --highlight-line %d", start, highlightLine+transcriptPreviewAfter, highlightLine)
	}

	cmd := fmt.Sprintf("bat %s '%s'; while :; do sleep 3600; done", args, path)
	m.tmux.RespawnPane(m.stagePane, home, "sh", "-c", cmd)
}

// StageBlank clears the stage back to an empty shell.
func (m *AgentManager) StageBlank() {
	m.ensureStage()
	if m.stagePane == "" {
		return
	}

	m.restoreStage()
	home, _ := os.UserHomeDir()
	m.tmux.RespawnPane(m.stagePane, home)
}

// Rename retitles an agent. The name is a store-only display label and nothing in
// tmux keys on it, so no tmux object is renamed.
func (m *AgentManager) Rename(agent store.Agent, name string) {
	name = store.SanitizeName(strings.TrimSpace(name))
	if name == "" || name == agent.Name {
		return
	}

	_, _ = m.sessions.Rename(context.Background(), agent.ID, name)
}

// Delete soft-deletes an agent's record and kills its pane first. If the agent is
// currently staged, it is swapped back out so the kill does not take the stage
// pane down with it.
func (m *AgentManager) Delete(agent store.Agent) {
	if m.stagedChatPane == agent.Pane {
		m.restoreStage()
	}

	m.tmux.KillPane(agent.Pane)
	_ = m.sessions.SoftDelete(context.Background(), agent.Name)
}

// NewSessionParams configures a background agent to create.
type NewSessionParams struct {
	Client string
	Title  string
	Dir    string
	Flags  string
	Prompt string
	// WorkflowID scopes the new chat to a workflow, or is nil when it is created
	// from the root and belongs to no workflow.
	WorkflowID *string
}

// NewSession creates a background session, records it so it lists immediately,
// and launches claude with the default plus per-session flags and opening
// prompt. It does not switch the client to the new session. The stored record is
// returned so a caller that did not watch the list, such as an RPC client, still
// learns the chat's generated name and id.
//
// The directory is the given Dir, else the client's current directory.
func (m *AgentManager) NewSession(p NewSessionParams) (*store.Agent, error) {
	dir := expandHome(strings.TrimSpace(p.Dir))
	if dir == "" {
		dir = m.tmux.ClientPath(p.Client)
	}
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}

	// tmux silently falls back to its own default directory when -c names
	// something it cannot enter, so an unusable directory produced a chat running
	// somewhere other than the cwd stored against it. Checking first means the
	// launch fails instead of recording a location that is not true.
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to use %q as the working directory: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("working directory %q is not a directory", dir)
	}

	// A chat created inside a workflow inherits the workflow's shared context,
	// injected as an appended system prompt at launch. It is a snapshot. Later
	// edits to the workflow context reach only sessions spawned after them. An
	// unknown workflow is refused rather than filed under an id that renders
	// nowhere.
	var workflowContext string
	if p.WorkflowID != nil {
		wf, err := m.sessions.GetWorkflow(context.Background(), *p.WorkflowID)
		if err != nil {
			return nil, fmt.Errorf("failed to look up workflow %q: %w", *p.WorkflowID, err)
		}
		if wf == nil {
			return nil, fmt.Errorf("no workflow with id %q", *p.WorkflowID)
		}

		workflowContext = wf.Context
	}

	name := m.uniqueSessionName(p.Title, p.Prompt)

	pane := m.tmux.NewWindow(ManagerSession, name, dir)
	flags := strings.TrimSpace(m.claudeFlags + " " + p.Flags)

	// Generate Claude's session id ourselves and launch with --session-id, so the
	// id is known up front and restoring the agent can resume that same
	// conversation with --resume.
	sessionID := uuid.NewString()
	if err := m.sessions.Upsert(context.Background(), store.Agent{
		Name:            name,
		Pane:            pane,
		Cwd:             dir,
		Flags:           flags,
		Prompt:          p.Prompt,
		State:           store.StateNew,
		ClaudeSessionID: sessionID,
		WorkflowID:      p.WorkflowID,
	}); err != nil {
		return nil, fmt.Errorf("failed to record session %q: %w", name, err)
	}

	m.launchClaude(launchClaudeParams{
		Session:         pane,
		Flags:           flags,
		Prompt:          p.Prompt,
		WorkflowContext: workflowContext,
		SessionID:       sessionID,
	})

	// Read the row back rather than returning what was written: the store owns
	// the generated id and the created timestamp.
	created, err := m.sessions.Get(context.Background(), name)
	if err != nil {
		return nil, fmt.Errorf("failed to read back session %q: %w", name, err)
	}
	if created == nil {
		return nil, fmt.Errorf("session %q vanished after being recorded", name)
	}

	return created, nil
}

// ListWorkflows returns every live workflow for the tree's folder headers.
func (m *AgentManager) ListWorkflows() ([]store.Workflow, error) {
	workflows, err := m.sessions.ListWorkflows(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to list workflows: %w", err)
	}

	return workflows, nil
}

// GetWorkflow returns a workflow by id, or nil when it does not exist. The panel
// uses it to restore the workflow scope when looping back after an attach.
func (m *AgentManager) GetWorkflow(id string) (*store.Workflow, error) {
	workflow, err := m.sessions.GetWorkflow(context.Background(), id)
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow %q: %w", id, err)
	}

	return workflow, nil
}

// NewWorkflow creates a workflow with its free-text context and default config,
// defaulting a blank name so the list always has a label. The name is a display
// label only and nothing keys on it. The cwd is home-expanded so the stored
// blueprint matches what a session launch would use.
func (m *AgentManager) NewWorkflow(cfg store.WorkflowConfig) (*store.Workflow, error) {
	cfg.Name = strings.TrimSpace(cfg.Name)
	if cfg.Name == "" {
		cfg.Name = "workflow"
	}

	cfg.Cwd = expandHome(strings.TrimSpace(cfg.Cwd))
	cfg.Flags = strings.TrimSpace(cfg.Flags)

	workflow, err := m.sessions.CreateWorkflow(context.Background(), cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create workflow %q: %w", cfg.Name, err)
	}

	return workflow, nil
}

// RenameWorkflow retitles a workflow by id. A blank or unchanged name is a no-op.
func (m *AgentManager) RenameWorkflow(workflow store.Workflow, name string) {
	name = strings.TrimSpace(name)
	if name == "" || name == workflow.Name {
		return
	}

	_ = m.sessions.RenameWorkflow(context.Background(), workflow.ID, name)
}

// UpdateWorkflow patches a workflow's display name, free-text context, and
// default config in place, keyed by id. A blank name falls back to the existing
// name so the list always has a label. The cwd is home-expanded so the stored
// blueprint matches what a session launch would use.
func (m *AgentManager) UpdateWorkflow(workflow store.Workflow, cfg store.WorkflowConfig) {
	cfg.Name = strings.TrimSpace(cfg.Name)
	if cfg.Name == "" {
		cfg.Name = workflow.Name
	}

	cfg.Cwd = expandHome(strings.TrimSpace(cfg.Cwd))
	cfg.Flags = strings.TrimSpace(cfg.Flags)

	_ = m.sessions.UpdateWorkflow(context.Background(), workflow.ID, cfg)
}

// DeleteWorkflow soft-deletes a workflow, orphaning its chats back to
// unassigned so none are stranded behind it.
func (m *AgentManager) DeleteWorkflow(workflow store.Workflow) {
	_ = m.sessions.DeleteWorkflow(context.Background(), workflow.ID)
}

// DeleteWorkflowWithChats soft-deletes a workflow together with its chats, so
// the whole bucket is removed at once rather than orphaning its chats.
func (m *AgentManager) DeleteWorkflowWithChats(workflow store.Workflow) {
	_ = m.sessions.DeleteWorkflowWithChats(context.Background(), workflow.ID)
}

// MoveChat reassigns a chat to a workflow, or unassigns it when workflowID is
// nil, keyed by the chat's stable id.
func (m *AgentManager) MoveChat(agent store.Agent, workflowID *string) {
	_ = m.sessions.MoveToWorkflow(context.Background(), agent.ID, workflowID)
}

// Restore recreates a dormant agent's tmux session from its stored cwd/flags
// and returns the new pane id, updating the stored record. A chat restored from
// search may be soft-deleted, so its deleted flag is cleared to return it to the
// normal listing.
func (m *AgentManager) Restore(agent store.Agent) (string, error) {
	dir := expandHome(agent.Cwd)
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}

	// A deleted agent's original name may have been reused by a live agent while
	// it sat deleted. Undeleting it under that name would collide on the
	// unique-name index and fail, so give it a fresh unique name first, renaming
	// the stored row by id. A non-deleted dormant agent already owns its name, so
	// it is left as is.
	if agent.Deleted && m.nameTaken(agent.Name) {
		newName := m.uniqueSessionName(agent.Name, "")
		if _, err := m.sessions.Rename(context.Background(), agent.ID, newName); err != nil {
			return "", fmt.Errorf("failed to rename restored session %q: %w", agent.Name, err)
		}

		agent.Name = newName
	}

	pane := m.tmux.NewWindow(ManagerSession, agent.Name, dir)

	// Clear the soft-delete flag first, so the row re-enters the unique-name
	// index before the upsert below keys off it. Upserting a still-deleted row
	// would collide on the id primary key rather than resolve as a name conflict.
	if agent.Deleted {
		if err := m.sessions.Undelete(context.Background(), agent.ID); err != nil {
			return "", fmt.Errorf("failed to undelete session %q: %w", agent.Name, err)
		}
	}

	agent.Pane = pane
	agent.State = store.StateNew
	if err := m.sessions.Upsert(context.Background(), agent); err != nil {
		return "", fmt.Errorf("failed to restore session %q: %w", agent.Name, err)
	}

	m.launchClaude(launchClaudeParams{
		Session:   pane,
		Flags:     agent.Flags,
		SessionID: agent.ClaudeSessionID,
		Resume:    true,
	})

	return pane, nil
}

// OpenSearchHit opens the chat a search hit points at, keyed by its stable id,
// and returns the live pane and the chat's current name. It spans every chat,
// including soft-deleted ones. An already-live chat is returned as is. A dormant
// or deleted one is restored first, which relaunches Claude on its saved session
// and clears the deleted flag. An unknown id is an error.
func (m *AgentManager) OpenSearchHit(id string) (string, string, error) {
	agents, err := m.sessions.ListAll(context.Background())
	if err != nil {
		return "", "", fmt.Errorf("failed to list agents to open search hit: %w", err)
	}

	for _, agent := range agents {
		if agent.ID != id {
			continue
		}
		if m.tmux.PaneAlive(agent.Pane) {
			return agent.Pane, agent.Name, nil
		}

		pane, err := m.Restore(agent)
		if err != nil {
			return "", "", err
		}

		// Restore may rename a deleted chat whose name was reused while it sat
		// deleted, so re-resolve the name by id rather than trusting the stale one.
		return pane, m.nameByID(context.Background(), id, agent.Name), nil
	}

	return "", "", fmt.Errorf("no chat with id %q", id)
}

// nameByID returns the current name of the chat with the given id, falling back
// to fallback when the lookup fails, so a caller can position on a chat whose
// name may have just changed.
func (m *AgentManager) nameByID(ctx context.Context, id, fallback string) string {
	agents, err := m.sessions.ListAll(ctx)
	if err != nil {
		return fallback
	}

	for _, agent := range agents {
		if agent.ID == id {
			return agent.Name
		}
	}

	return fallback
}

// uniqueSessionName derives a session name from the title, falling back to a
// slug of the prompt, then "agent", and appends a suffix until it is free.
func (m *AgentManager) uniqueSessionName(title, prompt string) string {
	base := store.SanitizeName(strings.TrimSpace(title))
	if base == "" {
		base = xstr.Slug(xstr.FirstLine(prompt), 24)
	}
	if base == "" {
		base = "agent"
	}

	name := base
	for i := 2; m.nameTaken(name); i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}

	return name
}

// nameTaken reports whether a name is already owned by a live agent record. The
// store's unique-name index is the single authority. Reusing a name would make
// the upsert clobber that agent's row instead of creating a fresh one.
func (m *AgentManager) nameTaken(name string) bool {
	taken, _ := m.sessions.ActiveNameExists(context.Background(), name)
	return taken
}

// mcpBriefing tells a spawned agent that the panel's own database is queryable,
// so a question about the other chats is something it can look up rather than
// guess at. The closing line matters as much as the rest: an agent whose client
// has not registered the server must say so instead of inventing an answer.
const mcpBriefing = `You are a background chat run by claude-agents, a tmux panel that spawns and tracks Claude sessions. The panel serves its own SQLite database read-only over MCP as a server named "claude-agents" at http://%s/mcp.

That server's tools are list_tables, describe_table, and query, which runs a single SELECT. Between them they cover every chat the panel knows about: sessions holds each chat's name, state, and pane, session_configs its working directory and flags, workflows the groups chats belong to, and session_transcripts the text of their conversations. Writes are rejected. Reach for these when the question is about the other chats rather than the code in front of you.

If those tools are not present in your session, the server is not registered with your client. Say that plainly rather than answering from guesswork.`

// mcpConfigJSON is the MCP client config every agent launches with, pointing at
// the panel's own read-only endpoint. It is passed with --mcp-config rather than
// --strict-mcp-config, so an agent gains this server on top of whatever the user
// has already registered instead of trading them for it.
const mcpConfigJSON = `{"mcpServers":{"claude-agents":{"type":"http","url":"http://%s/mcp"}}}`

// writeMCPConfig writes the MCP client config beside the panel's other state and
// returns its path. It is rewritten on every launch so a changed listen address
// takes effect without a stale file lingering, and unlike the prompt temp files
// it is not removed as the command runs: a resumed agent re-reads it at every
// relaunch.
func (m *AgentManager) writeMCPConfig() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve home directory: %w", err)
	}

	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create mcp config dir: %w", err)
	}

	path := filepath.Join(dir, "claude-agents-mcp.json")
	if err := os.WriteFile(path, []byte(fmt.Sprintf(mcpConfigJSON, m.serverAddr)), 0o644); err != nil {
		return "", fmt.Errorf("failed to write mcp config: %w", err)
	}

	return path, nil
}

// appendedSystemPrompt is the text a fresh launch appends to the system prompt:
// the standing MCP briefing, followed by the workflow's shared context when the
// chat belongs to one.
func (m *AgentManager) appendedSystemPrompt(workflowContext string) string {
	briefing := fmt.Sprintf(mcpBriefing, m.serverAddr)
	if strings.TrimSpace(workflowContext) == "" {
		return briefing
	}

	return briefing + "\n\n" + workflowContext
}

// launchClaudeParams configures a single claude launch.
type launchClaudeParams struct {
	Session         string
	Flags           string
	Prompt          string
	WorkflowContext string
	SessionID       string
	Resume          bool
}

// launchClaude types the claude command into a session. When Resume is set it
// resumes the prior conversation by session id. Otherwise it starts a new
// conversation pinned to that session id, with the opening prompt passed through
// a temp file so multi-line input survives, removed as the command runs.
func (m *AgentManager) launchClaude(params launchClaudeParams) {
	command := "claude"

	// Flags are data, not shell. They are split into arguments and quoted, so a
	// flag carrying $(...), a backtick, or a semicolon reaches claude as a
	// literal argument instead of running in the pane's shell. Flags can arrive
	// over RPC, which makes this the boundary between a request and the
	// user's machine.
	if quoted := quoteFlags(params.Flags); quoted != "" {
		command += " " + quoted
	}

	// Both paths get the MCP config: the briefing does not survive a resume, but
	// the tools it describes should. A failure to write it drops the flag rather
	// than the launch, leaving an agent that runs without the server.
	if path, err := m.writeMCPConfig(); err == nil {
		command += fmt.Sprintf(" --mcp-config '%s'", path)
	}

	// The session id is generated here, but a restored agent's comes back out of
	// the store, so it is quoted like anything else that was not built inline.
	switch {
	case params.Resume && params.SessionID != "":
		command += " --resume " + quoteArg(params.SessionID)
	default:
		if params.SessionID != "" {
			command += " --session-id " + quoteArg(params.SessionID)
		}
		// Pass the MCP briefing and any workflow context as an appended system
		// prompt, sourced through a temp file the shell cats in place so arbitrary
		// markdown needs no escaping. --append-system-prompt does not survive
		// --resume, matching the snapshot-at-spawn behavior.
		file, err := os.CreateTemp("", "claude-context-*.md")
		if err == nil {
			_, _ = file.WriteString(m.appendedSystemPrompt(params.WorkflowContext))
			_ = file.Close()
			command += fmt.Sprintf(" --append-system-prompt \"$(cat '%s'; rm '%s')\"", file.Name(), file.Name())
		}
		// The opening prompt is positional, and --mcp-config takes a variable
		// number of paths, so the prompt is fenced off with -- or a launch where
		// no flag separates the two would read the prompt as another config file.
		if params.Prompt != "" {
			file, err := os.CreateTemp("", "claude-prompt-*.md")
			if err == nil {
				_, _ = file.WriteString(params.Prompt)
				_ = file.Close()
				command += fmt.Sprintf(" -- \"$(cat '%s'; rm '%s')\"", file.Name(), file.Name())
			}
		}
	}

	m.tmux.SendKeys(params.Session, command, "Enter")
}

// Status renders the global agent summary for the tmux status bar.
func (m *AgentManager) Status() string {
	agents, err := m.sessions.List(context.Background())
	if err != nil {
		return ""
	}

	waiting, working, done := 0, 0, 0
	for _, agent := range agents {
		switch agent.State {
		case store.StateThinking:
			working++
		case store.StateWaiting:
			waiting++
		case store.StateDone:
			done++
		}
	}

	const base = "#[fg=#7E9CD8]"

	var segments []string
	if waiting > 0 {
		segments = append(segments, fmt.Sprintf("#[fg=#E82424]%d waiting%s", waiting, base))
	}
	if working > 0 {
		segments = append(segments, fmt.Sprintf("#[fg=#FF9E3B]%d working%s", working, base))
	}
	if done > 0 {
		segments = append(segments, fmt.Sprintf("#[fg=#98BB6C]%d done%s", done, base))
	}
	if len(segments) == 0 {
		return ""
	}

	return strings.Join(segments, "  ") + "  "
}
