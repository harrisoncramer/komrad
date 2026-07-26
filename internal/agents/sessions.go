package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"claude-agents/internal/store"
	"claude-agents/lib/xstr"

	"claude-agents/internal/store/session"
	"claude-agents/internal/store/transcript"
	"claude-agents/internal/store/workflow"

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

// sessionStore is the part of the chat store this service reads and mutates.
type sessionStore interface {
	List(ctx context.Context) ([]session.Agent, error)
	ListAll(ctx context.Context) ([]session.Agent, error)
	Get(ctx context.Context, name string) (*session.Agent, error)
	Find(ctx context.Context, name string) (*session.Target, error)
	Upsert(ctx context.Context, agent session.Agent) error
	Rename(ctx context.Context, id, name string) (bool, error)
	Archive(ctx context.Context, name string) error
	Restore(ctx context.Context, id string) error
	ActiveNameExists(ctx context.Context, name string) (bool, error)
	MoveToWorkflow(ctx context.Context, id string, workflowID *string) error
}

// workflowReader is the one thing this service needs from the workflow domain: a
// chat spawned inside a workflow snapshots that workflow's shared context at
// launch. It reads and never writes, which is why it is a single method rather
// than the whole workflow store.
type workflowReader interface {
	Get(ctx context.Context, id string) (*workflow.Workflow, error)
}

// txRunner opens the transaction a write spanning two domains needs. Removing a
// chat has to remove its transcript, which lives in another package, so the two
// stores are rebound onto one transaction rather than either reaching into the
// other's tables.
type txRunner interface {
	InTx(ctx context.Context, fn func(store.DBTX) error) error
}

// sessionTmux is the part of tmux a chat's lifecycle needs: a window to live in,
// a way to end it, and a way to tell whether it is still there.
type sessionTmux interface {
	NewWindow(session, name, dir string) (string, error)
	KillPane(pane string)
	PaneAlive(pane string) bool
	ClientPath(client string) string
}

// SessionService owns a chat's lifecycle: creating one, bringing a dormant one
// back, renaming, archiving, and removing it. It coordinates the store with tmux
// and delegates the command line to the launcher.
type SessionService struct {
	tmux      sessionTmux
	sessions  sessionStore
	workflows workflowReader
	tx        txRunner
	launcher  *Launcher
	// claudeFlags are the configured defaults every chat launches with, recorded
	// on the row alongside whatever the caller added.
	claudeFlags string
}

// SessionParams carries the session service's dependencies.
type SessionParams struct {
	Tmux        sessionTmux
	Sessions    sessionStore
	Workflows   workflowReader
	Tx          txRunner
	Launcher    *Launcher
	ClaudeFlags string
}

// NewSessionService builds the service from its dependencies.
func NewSessionService(p SessionParams) *SessionService {
	return &SessionService{
		tmux:        p.Tmux,
		sessions:    p.Sessions,
		workflows:   p.Workflows,
		tx:          p.Tx,
		launcher:    p.Launcher,
		claudeFlags: p.ClaudeFlags,
	}
}

// PaneAlive reports whether a chat's tmux pane still exists.
func (s *SessionService) PaneAlive(pane string) bool {
	return s.tmux.PaneAlive(pane)
}

// ErrSessionNotFound reports that no chat answers to the given name or id.
var ErrSessionNotFound = errors.New("session not found")

// ErrWorkflowNotFound reports that a chat named a workflow that does not exist.
var ErrWorkflowNotFound = errors.New("workflow not found")

// ErrBadDirectory reports a working directory the agent could not be started in.
var ErrBadDirectory = errors.New("unusable working directory")

// DeleteSession stops a chat and removes it. Killing the pane first is the part
// the store cannot do: deleting the row alone left the agent running, with
// nothing left that could name it or reach it.
func (s *SessionService) DeleteSession(ctx context.Context, name string) error {
	agent, err := s.sessions.Get(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to look up session %q: %w", name, err)
	}
	if agent == nil {
		return ErrSessionNotFound
	}

	s.tmux.KillPane(agent.Pane)

	return s.removeChat(ctx, name)
}

// removeChat deletes a chat, the config it owns, and the transcript keyed to its
// id, in one transaction. The transcript is the transcript package's table, so
// the two stores are rebound onto the same transaction and commit together: a
// chat removed without its text leaves the conversation readable through the SQL
// tools, which is the half-state this avoids.
func (s *SessionService) removeChat(ctx context.Context, name string) error {
	target, err := s.sessions.Find(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to find session %q to delete: %w", name, err)
	}
	if target == nil {
		return ErrSessionNotFound
	}

	return s.tx.InTx(ctx, func(db store.DBTX) error {
		if err := session.New(db).Delete(ctx, *target); err != nil {
			return err
		}

		return transcript.New(db).Delete(ctx, target.ID)
	})
}

// ArchiveSession stops a chat and hides it from the panel, keyed by name. The
// record and its transcript stay, so search still reaches it and it can be
// restored later. This is what the panel's delete key does, as distinct from
// DeleteSession, which removes the chat for good.
func (s *SessionService) ArchiveSession(ctx context.Context, name string) error {
	agent, err := s.sessions.Get(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to look up session %q: %w", name, err)
	}
	if agent == nil {
		return ErrSessionNotFound
	}

	s.tmux.KillPane(agent.Pane)

	if err := s.sessions.Archive(ctx, name); err != nil {
		return fmt.Errorf("failed to archive session %q: %w", name, err)
	}

	return nil
}

// RestoreSession brings a chat back by id, relaunching claude on the same
// conversation, and returns it as stored. It spans archived chats, since
// restoring one is the point. An id that matches nothing is ErrSessionNotFound.
func (s *SessionService) RestoreSession(ctx context.Context, id string) (*session.Agent, error) {
	agents, err := s.sessions.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions to restore %q: %w", id, err)
	}

	for _, agent := range agents {
		if agent.ID != id {
			continue
		}

		// A chat whose pane is still alive is already running. Returning it as it
		// stands keeps the call idempotent rather than spawning a second window
		// for the same conversation.
		if s.tmux.PaneAlive(agent.Pane) {
			return &agent, nil
		}

		if err := s.restore(ctx, agent); err != nil {
			return nil, err
		}

		// Restore may rename a chat whose name was taken while it sat archived, so
		// the row is read back rather than returned from the stale copy.
		restored, err := s.byID(ctx, id)
		if err != nil {
			return nil, err
		}

		return restored, nil
	}

	return nil, ErrSessionNotFound
}

// byID returns the chat with the given id, spanning archived ones.
func (s *SessionService) byID(ctx context.Context, id string) (*session.Agent, error) {
	agents, err := s.sessions.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}

	for _, agent := range agents {
		if agent.ID == id {
			return &agent, nil
		}
	}

	return nil, ErrSessionNotFound
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

// usableDir reports whether an agent can actually be started in dir. Existence
// is not enough: tmux falls back to its own default directory when it cannot
// enter the one given to -c, and it does that silently, so a directory that
// cannot be opened would produce a chat running somewhere other than the cwd
// stored against it.
func usableDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("%w %q: %w", ErrBadDirectory, dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %q is not a directory", ErrBadDirectory, dir)
	}

	handle, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("%w %q: %w", ErrBadDirectory, dir, err)
	}
	_ = handle.Close()

	return nil
}

// NewSession creates a background session, records it so it lists immediately,
// and launches claude with the default plus per-session flags and opening
// prompt. It does not switch the client to the new session. The stored record is
// returned so a caller that did not watch the list, such as an RPC client, still
// learns the chat's generated name and id.
//
// The directory is the given Dir, else the client's current directory.
func (s *SessionService) NewSession(ctx context.Context, p NewSessionParams) (*session.Agent, error) {
	dir := expandHome(strings.TrimSpace(p.Dir))
	if dir == "" {
		dir = s.tmux.ClientPath(p.Client)
	}
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}

	// tmux silently falls back to its own default directory when -c names
	// something it cannot enter, so an unusable directory produced a chat running
	// somewhere other than the cwd stored against it. Checking first means the
	// launch fails instead of recording a location that is not true.
	if err := usableDir(dir); err != nil {
		return nil, err
	}

	// A chat created inside a workflow inherits the workflow's shared context,
	// injected as an appended system prompt at launch. It is a snapshot. Later
	// edits to the workflow context reach only sessions spawned after them. An
	// unknown workflow is refused rather than filed under an id that renders
	// nowhere.
	var workflowContext string
	if p.WorkflowID != nil {
		wf, err := s.workflows.Get(ctx, *p.WorkflowID)
		if err != nil {
			return nil, fmt.Errorf("failed to look up workflow %q: %w", *p.WorkflowID, err)
		}
		if wf == nil {
			return nil, fmt.Errorf("%w: %q", ErrWorkflowNotFound, *p.WorkflowID)
		}

		workflowContext = wf.Context
	}

	name := s.uniqueSessionName(ctx, p.Title, p.Prompt)

	pane, err := s.tmux.NewWindow(ManagerSession, name, dir)
	if err != nil {
		return nil, fmt.Errorf("failed to open a window for %q: %w", name, err)
	}
	flags := strings.TrimSpace(s.claudeFlags + " " + p.Flags)

	// Generate Claude's session id ourselves and launch with --session-id, so the
	// id is known up front and restoring the agent can resume that same
	// conversation with --resume.
	sessionID := uuid.NewString()
	if err := s.sessions.Upsert(ctx, session.Agent{
		Name:            name,
		Pane:            pane,
		Cwd:             dir,
		Flags:           flags,
		Prompt:          p.Prompt,
		State:           session.StateNew,
		ClaudeSessionID: sessionID,
		WorkflowID:      p.WorkflowID,
	}); err != nil {
		// uniqueSessionName checks before inserting, so two calls racing on the
		// same title both pass that check and one loses here. The window is
		// already open, and nothing else knows about it: without this kill it
		// would run forever, invisible to every listing.
		s.tmux.KillPane(pane)

		return nil, fmt.Errorf("failed to record session %q: %w", name, err)
	}

	if err := s.launcher.launch(launchClaudeParams{
		Session:         pane,
		Flags:           flags,
		Prompt:          p.Prompt,
		WorkflowContext: workflowContext,
		SessionID:       sessionID,
	}); err != nil {
		// The row is already stored, and the pane is open with nothing useful in
		// it. Left alone it would list as a live chat forever, so both go.
		s.tmux.KillPane(pane)
		if deleteErr := s.removeChat(ctx, name); deleteErr != nil {
			return nil, fmt.Errorf("failed to launch claude for %q (%w), and failed to clean up: %w", name, err, deleteErr)
		}

		return nil, fmt.Errorf("failed to launch claude for %q: %w", name, err)
	}

	// Read the row back rather than returning what was written: the store owns
	// the generated id and the created timestamp.
	created, err := s.sessions.Get(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("failed to read back session %q: %w", name, err)
	}
	if created == nil {
		return nil, fmt.Errorf("session %q vanished after being recorded", name)
	}

	return created, nil
}

// restore recreates a dormant agent's tmux window from its stored cwd and flags,
// updating the record. An archived chat has its hidden flag cleared, which is
// what returns it to the normal listing.
func (s *SessionService) restore(ctx context.Context, agent session.Agent) error {
	dir := expandHome(agent.Cwd)
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}

	// A deleted agent's original name may have been reused by a live agent while
	// it sat deleted. Undeleting it under that name would collide on the
	// unique-name index and fail, so give it a fresh unique name first, renaming
	// the stored row by id. A non-deleted dormant agent already owns its name, so
	// it is left as is.
	if agent.Deleted && s.nameTaken(ctx, agent.Name) {
		newName := s.uniqueSessionName(ctx, agent.Name, "")
		if _, err := s.sessions.Rename(ctx, agent.ID, newName); err != nil {
			return fmt.Errorf("failed to rename restored session %q: %w", agent.Name, err)
		}

		agent.Name = newName
	}

	pane, err := s.tmux.NewWindow(ManagerSession, agent.Name, dir)
	if err != nil {
		return fmt.Errorf("failed to open a window for %q: %w", agent.Name, err)
	}

	// Clear the soft-delete flag first, so the row re-enters the unique-name
	// index before the upsert below keys off it. Upserting a still-deleted row
	// would collide on the id primary key rather than resolve as a name conflict.
	if agent.Deleted {
		if err := s.sessions.Restore(ctx, agent.ID); err != nil {
			return fmt.Errorf("failed to unarchive session %q: %w", agent.Name, err)
		}
	}

	agent.Pane = pane
	agent.State = session.StateNew
	if err := s.sessions.Upsert(ctx, agent); err != nil {
		return fmt.Errorf("failed to restore session %q: %w", agent.Name, err)
	}

	if err := s.launcher.launch(launchClaudeParams{
		Session:   pane,
		Flags:     agent.Flags,
		SessionID: agent.ClaudeSessionID,
		Resume:    true,
	}); err != nil {
		return fmt.Errorf("failed to relaunch claude for %q: %w", agent.Name, err)
	}

	return nil
}

// uniqueSessionName derives a session name from the title, falling back to a
// slug of the prompt, then "agent", and appends a suffix until it is free.
func (s *SessionService) uniqueSessionName(ctx context.Context, title, prompt string) string {
	base := session.SanitizeName(strings.TrimSpace(title))
	if base == "" {
		base = xstr.Slug(xstr.FirstLine(prompt), 24)
	}
	if base == "" {
		base = "agent"
	}

	name := base
	for i := 2; s.nameTaken(ctx, name); i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}

	return name
}

// nameTaken reports whether a name is already owned by a live agent record. The
// store's unique-name index is the single authority. Reusing a name would make
// the upsert clobber that agent's row instead of creating a fresh one.
func (s *SessionService) nameTaken(ctx context.Context, name string) bool {
	taken, _ := s.sessions.ActiveNameExists(ctx, name)
	return taken
}
