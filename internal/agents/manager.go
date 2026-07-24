// Package agents orchestrates the store and tmux to implement both the panel
// actions and the CLI subcommands (new, notify, status, serve).
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

// expandHome expands a leading ~ or ~/ in a directory to the user's home. tmux's
// new-session -c takes the start directory literally and does no shell tilde
// expansion, so a path like "~/chariot" would fail to chdir and silently fall
// back to the default directory. Expanding here keeps the stored cwd absolute.
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

// tmuxClient is the slice of the tmux client the manager drives. Accepting it as
// an interface lets tests substitute a recording fake for the real CLI.
type tmuxClient interface {
	Socket() string
	SetGlobal(name, value string)
	PaneAlive(pane string) bool
	HasSession(name string) bool
	SessionAttached(session string) bool
	ResizeWindow(target string, width, height int)
	CapturePane(pane string) string
	RenameSession(oldName, newName string)
	ClientSession(client string) string
	SwitchToLast(client string)
	KillSession(name string)
	ClientPath(client string) string
	NewDetachedSession(name, dir string)
	PaneID(target string) (string, error)
	SendKeys(target string, keys ...string)
	CurrentSession() (string, error)
	SetWindowSizeLatest(target string)
	ShowGlobal(name string) string
	DetachSession(name string)
	SetSessionOption(target, name, value string)
	CurrentPane() (string, error)
	SplitRight(anchorPane, dir string) string
	NewWindow(session, name, dir string) string
	SwapPanes(src, dst string)
	RespawnPane(pane, dir string, argv ...string)
	KillPane(pane string)
}

// agentStore is the slice of the store the manager reads and mutates. Accepting
// it as an interface keeps the manager's business logic testable against a real
// store without pulling in a concrete type.
type agentStore interface {
	List(ctx context.Context) ([]store.Agent, error)
	ListAll(ctx context.Context) ([]store.Agent, error)
	Upsert(ctx context.Context, agent store.Agent) error
	UpdateState(ctx context.Context, name, pane, state, claudeSessionID string) error
	SetState(ctx context.Context, name, state string) error
	SetStateByID(ctx context.Context, id, state string) error
	Rename(ctx context.Context, id, name string) error
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

	// The stage is the pane beside the panel that shows the hovered resource.
	// navPane anchors the split; stagePane is the pane the panel drives (bat, or a
	// blank shell); stagedChatPane is the agent pane currently swapped into the
	// slot, or empty when the stage shows the stagePane itself. Set up by InitStage
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
}

// NewAgentManager constructs an AgentManager from its dependencies.
func NewAgentManager(p Params) *AgentManager {
	return &AgentManager{
		tmux:        p.Tmux,
		sessions:    p.Store,
		selfPath:    p.SelfPath,
		claudeFlags: p.ClaudeFlags,
	}
}

// ManagerSession is the name of the persistent tmux session the manager panel
// runs in. The popup attaches to it rather than launching the panel fresh, so
// hiding the popup (detaching this session's client) leaves the panel running
// and reopening restores it exactly. The tmux plugin uses the same literal.
const ManagerSession = "claude-manager"

// managerOpenFlag marks that the popup is currently shown, so the binding hides
// it (detaches) rather than opening a second one. The open command sets it while
// the popup is up and clears it when the popup closes. The tmux plugin reads it.
const managerOpenFlag = "@claude_manager_open"

// managerClientFlag holds the tmux client that opened the current popup. The
// open command writes it on each open, and the manager reads it live so actions
// that need the invoking client (a new chat's default directory, switching a
// client off a chat being deleted) use the client that is actually viewing the
// popup, not one fixed when the persistent panel first started.
const managerClientFlag = "@claude_manager_client"

// MarkOpen and MarkClosed toggle the popup-shown flag around the open command's
// blocking attach.
func (m *AgentManager) MarkOpen()   { m.tmux.SetGlobal(managerOpenFlag, "1") }
func (m *AgentManager) MarkClosed() { m.tmux.SetGlobal(managerOpenFlag, "0") }

// SetInvokingClient records the tmux client that opened the current popup.
func (m *AgentManager) SetInvokingClient(client string) {
	m.tmux.SetGlobal(managerClientFlag, client)
}

// managerAgentFlag holds the agent session currently attached inside the popup,
// or is empty when the picker is showing. The back binding reads it to detach
// just that agent's client, dropping back to the picker without closing the
// popup. esc cannot do this from inside a chat, since there it belongs to Claude.
const managerAgentFlag = "@claude_manager_agent"

// SetAttachedAgent and ClearAttachedAgent track which agent the popup is inside,
// around the blocking attach.
func (m *AgentManager) SetAttachedAgent(session string) { m.tmux.SetGlobal(managerAgentFlag, session) }
func (m *AgentManager) ClearAttachedAgent()             { m.tmux.SetGlobal(managerAgentFlag, "") }

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
// one when the stage is missing or has died — e.g. a one-shot preview process
// (bat) exited and tmux closed its pane. This self-heals the stage without a
// panel restart. A stage pane swapped out to hold a chat is still alive, so it
// is left as is.
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
// shows beside the panel. Focus stays on the panel; moving to the chat is a
// plain tmux pane move the user drives. A no-op when that chat is already staged.
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
// bat there. Used for a workflow's info and for a dormant chat's placeholder.
// bat is one-shot, so it is wrapped in a shell that holds the pane open after it
// prints — otherwise the process would exit and tmux would close the stage pane.
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

// Rename retitles an agent. The name is now a store-only display label; nothing
// in tmux keys on it, so no tmux object is renamed.
func (m *AgentManager) Rename(agent store.Agent, name string) {
	name = store.SanitizeName(strings.TrimSpace(name))
	if name == "" || name == agent.Name {
		return
	}

	_ = m.sessions.Rename(context.Background(), agent.ID, name)
}

// Delete soft-deletes an agent's record (hiding it from the panel), killing its
// pane first. If the agent is currently staged, it is swapped back out so the
// kill does not take the stage pane down with it.
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
// prompt. It does not switch the client to the new session.
//
// The directory is the given Dir, else the client's current directory.
func (m *AgentManager) NewSession(p NewSessionParams) {
	dir := expandHome(strings.TrimSpace(p.Dir))
	if dir == "" {
		dir = m.tmux.ClientPath(p.Client)
	}
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}

	name := m.uniqueSessionName(p.Title, p.Prompt)

	pane := m.tmux.NewWindow(ManagerSession, name, dir)
	flags := strings.TrimSpace(m.claudeFlags + " " + p.Flags)

	// A chat created inside a workflow inherits the workflow's shared context,
	// injected as an appended system prompt at launch. It is a snapshot: later
	// edits to the workflow context reach only sessions spawned after them.
	var workflowContext string
	if p.WorkflowID != nil {
		if wf, err := m.sessions.GetWorkflow(context.Background(), *p.WorkflowID); err == nil && wf != nil {
			workflowContext = wf.Context
		}
	}

	// Generate Claude's session id ourselves and launch with --session-id, so the
	// id is known up front, and restoring the agent can resume that same
	// conversation with --resume.
	sessionID := uuid.NewString()
	_ = m.sessions.Upsert(context.Background(), store.Agent{
		Name:            name,
		Pane:            pane,
		Cwd:             dir,
		Flags:           flags,
		Prompt:          p.Prompt,
		State:           store.StateNew,
		ClaudeSessionID: sessionID,
		WorkflowID:      p.WorkflowID,
	})

	m.launchClaude(pane, flags, p.Prompt, workflowContext, sessionID, false)
}

// ListWorkflows returns every live workflow for the workflow picker.
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

// NewWorkflow creates a workflow with its free-text context and default config
// (cwd, flags), defaulting a blank name so the picker always has a label to
// show. The name is a display label only; nothing keys on it. The cwd is
// home-expanded so the stored blueprint matches what a session launch would use.
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
// default config (cwd, flags) in place, keyed by id. A blank name falls back to
// the existing name so the picker always has a label. The cwd is home-expanded
// so the stored blueprint matches what a session launch would use, mirroring
// NewWorkflow.
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
	// the stored row by id. A non-deleted dormant agent already owns its name (its
	// own live row holds it), so it is left as is.
	if agent.Deleted && m.nameTaken(agent.Name) {
		newName := m.uniqueSessionName(agent.Name, "")
		if err := m.sessions.Rename(context.Background(), agent.ID, newName); err != nil {
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

	m.launchClaude(pane, agent.Flags, "", "", agent.ClaudeSessionID, true)

	return pane, nil
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

// nameTaken reports whether a name is unavailable for a new agent: a live agent
// record already owns it. The name is a store-only label now, so the store's
// unique-name index is the single authority; reusing a name would make the
// upsert clobber that agent's row instead of creating a fresh one.
func (m *AgentManager) nameTaken(name string) bool {
	taken, _ := m.sessions.ActiveNameExists(context.Background(), name)
	return taken
}

// launchClaude types the claude command into a session. When resume is set it
// resumes the prior conversation by session id; otherwise it starts a new
// conversation pinned to that session id, with the opening prompt passed through
// a temp file so multi-line input survives, removed as the command runs.
func (m *AgentManager) launchClaude(session, flags, prompt, workflowContext, sessionID string, resume bool) {
	command := "claude"
	if flags != "" {
		command += " " + flags
	}

	switch {
	case resume && sessionID != "":
		command += " --resume " + sessionID
	default:
		if sessionID != "" {
			command += " --session-id " + sessionID
		}
		// Pass the workflow context as an appended system prompt, sourced through a
		// temp file the shell cats in place so arbitrary markdown needs no escaping,
		// mirroring the prompt below. --append-system-prompt does not survive
		// --resume, matching the snapshot-at-spawn behavior.
		if workflowContext != "" {
			file, err := os.CreateTemp("", "claude-context-*.md")
			if err == nil {
				_, _ = file.WriteString(workflowContext)
				_ = file.Close()
				command += fmt.Sprintf(" --append-system-prompt \"$(cat '%s'; rm '%s')\"", file.Name(), file.Name())
			}
		}
		if prompt != "" {
			file, err := os.CreateTemp("", "claude-prompt-*.md")
			if err == nil {
				_, _ = file.WriteString(prompt)
				_ = file.Close()
				command += fmt.Sprintf(" \"$(cat '%s'; rm '%s')\"", file.Name(), file.Name())
			}
		}
	}

	m.tmux.SendKeys(session, command, "Enter")
}

// Notify records that an agent finished. It upserts state only, so a
// tool-created agent keeps its captured cwd/flags/prompt.
func (m *AgentManager) Notify(mainSession, pane, _ string) {
	key := m.agentKey(mainSession)
	if key == "" {
		return
	}

	_ = m.sessions.UpdateState(context.Background(), key, pane, store.StateDone, "")
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

func (m *AgentManager) agentKey(mainSession string) string {
	if mainSession != "" {
		return mainSession
	}

	session, _ := m.tmux.CurrentSession()
	return session
}

func envWithout(key string) []string {
	prefix := key + "="
	full := os.Environ()
	filtered := make([]string, 0, len(full))
	for _, entry := range full {
		if !strings.HasPrefix(entry, prefix) {
			filtered = append(filtered, entry)
		}
	}

	return filtered
}
