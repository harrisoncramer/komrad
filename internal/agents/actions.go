package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"weave/config"
	"weave/internal/actions"
	"weave/internal/logging"
	"weave/internal/tmux"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
)

type agentGetter interface {
	GetByID(ctx context.Context, id string) (*agentstore.Agent, error)
}

type tmuxReader interface {
	GetPaneOwner(pane string) string
	GetHostSocket() string
	GetInfoPane() string
	ShowGlobal(name string) string
	DefaultShell() string
}

type actionRunner interface {
	Run(p actions.RunParams) error
	RunAndWait(ctx context.Context, p actions.RunParams) (actions.Result, error)
}

type configLoader func() (*config.Config, error)

const (
	// TriggerManual identifies an action started directly by a person or model.
	TriggerManual = "manual"
	// TriggerSetup identifies an action run before an agent is created.
	TriggerSetup = "setup"
	// TriggerResume identifies a setup action rerun before an agent resumes.
	TriggerResume = "resume"
	// TriggerCleanup identifies an action run after an agent is deleted.
	TriggerCleanup = "cleanup"
)

// ActionService lists and runs built-in and user-defined actions.
type ActionService struct {
	agents     agentGetter
	workflows  workflowReader
	tmux       tmuxReader
	runner     actionRunner
	load       configLoader
	addr       string
	socket     string
	builtinDir string
	editor     string
	binPath    string

	catalog *actions.Catalog
}

// ActionParams carries the action service's dependencies.
type ActionParams struct {
	Agents     agentGetter
	Workflows  workflowReader
	Tmux       tmuxReader
	Runner     actionRunner
	Load       configLoader
	Addr       string
	Socket     string
	BuiltinDir string
	Editor     string
	BinPath    string
}

// NewActionService builds the service from its dependencies.
func NewActionService(p ActionParams) *ActionService {
	return &ActionService{
		agents:     p.Agents,
		workflows:  p.Workflows,
		tmux:       p.Tmux,
		runner:     p.Runner,
		load:       p.Load,
		addr:       p.Addr,
		socket:     p.Socket,
		builtinDir: p.BuiltinDir,
		editor:     p.Editor,
		binPath:    p.BinPath,
		catalog:    actions.NewCatalog(),
	}
}

// Exec arguments and environment variables share a kernel size limit.
const maxActionPrompt = 16 * 1024

// ListActions returns the available actions in name order.
func (s *ActionService) ListActions() ([]actions.Action, error) {
	dirs, err := s.actionDirs()
	if err != nil {
		return nil, err
	}

	return s.catalog.List(dirs...)
}

// RunAction starts an action for an agent without waiting for it to finish.
func (s *ActionService) RunAction(ctx context.Context, agentID, name string) error {
	agent, err := s.agents.GetByID(ctx, agentID)
	if err != nil {
		return fmt.Errorf("failed to read agent %q: %w", agentID, err)
	}
	if agent == nil {
		return ErrAgentNotFound
	}

	action, env, err := s.prepare(ctx, prepareParams{agent: *agent, name: name, trigger: TriggerManual})
	if err != nil {
		return err
	}

	return s.start(ctx, startParams{
		action:  action,
		env:     env,
		agent:   *agent,
		trigger: TriggerManual,
	})
}

// ErrNotEditable reports that an action is not a text file.
var ErrNotEditable = errors.New("the file is not text, so there is nothing to edit")

const editActionName = "_edit_action"

const editPathVar = "WEAVE_EDIT_PATH"

// EditAction opens a text action in the configured editor.
func (s *ActionService) EditAction(ctx context.Context, agentID, name string) error {
	agent, err := s.agents.GetByID(ctx, agentID)
	if err != nil {
		return fmt.Errorf("failed to read agent %q: %w", agentID, err)
	}
	if agent == nil {
		return ErrAgentNotFound
	}

	target, err := s.find(name)
	if err != nil {
		return err
	}
	if !target.Text {
		return fmt.Errorf("failed to edit action %s: %w", name, ErrNotEditable)
	}

	action, env, err := s.prepare(ctx, prepareParams{agent: *agent, name: editActionName, trigger: TriggerManual})
	if err != nil {
		return err
	}

	return s.start(ctx, startParams{
		action:  action,
		env:     append(env, editPathVar+"="+target.Path),
		agent:   *agent,
		trigger: TriggerManual,
	})
}

// ErrSetupFailed reports that an agent setup action failed.
var ErrSetupFailed = errors.New("the setup action failed")

const setupTimeout = 10 * time.Minute

const maxSetupDir = 4096

// setupDirFileVar names the environment variable a setup or resume action is handed, pointing
// at an empty file it may write the agent's new directory into. This is a channel of its own
// rather than stdout, since stdout is also where a script's own output and whatever it shells
// out to land, and any of that arriving first would otherwise be taken as the answer.
const setupDirFileVar = "WEAVE_SETUP_DIR_FILE"

// RunSetupAction runs a setup action before creating an agent.
func (s *ActionService) RunSetupAction(ctx context.Context, agent agentstore.Agent, name string) (string, error) {
	return s.runAwaitedAction(ctx, prepareParams{agent: agent, name: name, trigger: TriggerSetup})
}

// RunResumeAction reruns a setup action before resuming an agent.
func (s *ActionService) RunResumeAction(ctx context.Context, agent agentstore.Agent, name string) (string, error) {
	return s.runAwaitedAction(ctx, prepareParams{agent: agent, name: name, trigger: TriggerResume})
}

func (s *ActionService) runAwaitedAction(ctx context.Context, p prepareParams) (string, error) {
	action, env, err := s.prepare(ctx, p)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrSetupFailed, err)
	}

	dirFile, err := os.CreateTemp("", "weave-setup-dir-*")
	if err != nil {
		return "", fmt.Errorf("%w: failed to prepare a place for %s to name its directory: %w", ErrSetupFailed, p.name, err)
	}
	dirPath := dirFile.Name()
	closeErr := dirFile.Close()
	defer func() {
		if removeErr := os.Remove(dirPath); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			s.actionLogger(ctx, p.name, p.trigger, p.agent).LogAttrs(ctx, slog.LevelWarn,
				"failed to remove the setup action's directory file", logging.Err(removeErr)...)
		}
	}()
	if closeErr != nil {
		return "", fmt.Errorf("%w: failed to prepare a place for %s to name its directory: %w", ErrSetupFailed, p.name, closeErr)
	}

	env = append(env, setupDirFileVar+"="+dirPath)

	logger := s.actionLogger(ctx, p.name, p.trigger, p.agent)
	logger.LogAttrs(ctx, slog.LevelInfo, "action started")
	began := time.Now()

	result, err := s.runner.RunAndWait(ctx, actions.RunParams{
		Action:  action,
		Dir:     action.GetDir(),
		Env:     env,
		Timeout: action.Header.TimeoutOr(setupTimeout),
	})

	finished := []slog.Attr{
		slog.Int(logging.ExitCode, result.ExitCode),
		slog.Int64(logging.DurationMS, elapsedMS(began)),
	}
	if said := bytes.TrimSpace(result.Stderr); len(said) > 0 {
		finished = append(finished, slog.String(logging.ActionStderr, string(said)))
	}

	logger.LogAttrs(ctx, slog.LevelInfo, "action finished", finished...)

	if err != nil {
		return "", fmt.Errorf("%w: %w%s", ErrSetupFailed, err, stderrDetail(result.Stderr))
	}

	named, err := os.ReadFile(dirPath)
	if err != nil {
		return "", fmt.Errorf("%w: failed to read the directory %s named: %w", ErrSetupFailed, p.name, err)
	}

	answered := string(bytes.TrimSpace(named))
	if len(answered) > maxSetupDir {
		return "", fmt.Errorf("%w: %s wrote %d bytes to %s, where a directory to run in was expected", ErrSetupFailed, p.name, len(answered), setupDirFileVar)
	}

	return answered, nil
}

// RunCleanupAction starts a cleanup action without waiting for it to finish.
func (s *ActionService) RunCleanupAction(ctx context.Context, agent agentstore.Agent, name string) error {
	action, env, err := s.prepare(ctx, prepareParams{agent: agent, name: name, trigger: TriggerCleanup})
	if err != nil {
		return err
	}

	return s.start(ctx, startParams{
		action:  action,
		env:     env,
		agent:   agent,
		trigger: TriggerCleanup,
	})
}

type startParams struct {
	action  actions.Action
	env     []string
	agent   agentstore.Agent
	trigger string
}

func (s *ActionService) start(ctx context.Context, p startParams) error {
	logger := s.actionLogger(ctx, p.action.Name, p.trigger, p.agent)
	ctx = context.WithoutCancel(ctx)
	began := time.Now()

	err := s.runner.Run(actions.RunParams{
		Action: p.action,
		Dir:    p.action.GetDir(),
		Env:    p.env,
		// Nothing is waiting, so an action that declared no timeout runs as long as it likes.
		Timeout: p.action.Header.TimeoutOr(0),
		Done: func(result actions.Result, err error) {
			if err != nil {
				logger.LogAttrs(ctx, slog.LevelError, "action failed",
					append(failedAction(result, err), slog.Int64(logging.DurationMS, elapsedMS(began)))...)

				return
			}

			logger.LogAttrs(ctx, slog.LevelInfo, "action finished",
				slog.Int(logging.ExitCode, result.ExitCode),
				slog.Int64(logging.DurationMS, elapsedMS(began)))
		},
	})
	if err != nil {
		return err
	}

	logger.LogAttrs(ctx, slog.LevelInfo, "action started")

	return nil
}

func (s *ActionService) actionLogger(ctx context.Context, name, trigger string, agent agentstore.Agent) *slog.Logger {
	return logging.From(ctx).With(logging.Attrs([]slog.Attr{
		slog.String(logging.ActionName, name),
		slog.String(logging.ActionTrigger, trigger),
		slog.String(logging.ActionAgentID, agent.ID),
		slog.String(logging.ActionAgentCwd, agent.Cwd),
	})...)
}

func elapsedMS(began time.Time) int64 {
	return time.Since(began).Milliseconds()
}

func failedAction(result actions.Result, err error) []slog.Attr {
	attrs := []slog.Attr{slog.Int(logging.ExitCode, result.ExitCode)}
	attrs = append(attrs, logging.Err(err)...)

	if said := bytes.TrimSpace(result.Stderr); len(said) > 0 {
		attrs = append(attrs, slog.String(logging.ActionStderr, string(said)))
	}

	return attrs
}

func stderrDetail(stderr []byte) string {
	trimmed := bytes.TrimSpace(stderr)
	if len(trimmed) == 0 {
		return ""
	}

	return ": " + string(trimmed)
}

type prepareParams struct {
	agent   agentstore.Agent
	name    string
	trigger string
}

func (s *ActionService) prepare(ctx context.Context, p prepareParams) (actions.Action, []string, error) {
	action, err := s.find(p.name)
	if err != nil {
		return actions.Action{}, nil, err
	}

	env, err := s.env(ctx, actionEnvParams{agent: p.agent, dir: action.GetDir(), trigger: p.trigger, action: action.Name})
	if err != nil {
		return actions.Action{}, nil, err
	}

	return action, env, nil
}

func (s *ActionService) find(name string) (actions.Action, error) {
	dirs, err := s.actionDirs()
	if err != nil {
		return actions.Action{}, err
	}

	return s.catalog.Find(name, dirs...)
}

func (s *ActionService) actionDirs() ([]string, error) {
	cfg, err := s.load()
	if err != nil {
		return nil, fmt.Errorf("failed to read the config: %w", err)
	}

	userDir, err := cfg.GetActionsDir()
	if err != nil {
		return nil, err
	}

	return []string{userDir, s.builtinDir}, nil
}

type actionPayload struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	State           string            `json:"state"`
	Alive           bool              `json:"alive"`
	Cwd             string            `json:"cwd"`
	Harness         string            `json:"harness"`
	SessionID       string            `json:"session_id"`
	Flags           string            `json:"flags"`
	Prompt          string            `json:"prompt"`
	PromptTruncated bool              `json:"prompt_truncated"`
	Pane            string            `json:"pane"`
	Workflow        string            `json:"workflow"`
	Tag             string            `json:"tag"`
	Hidden          bool              `json:"hidden"`
	CreatedAt       string            `json:"created_at"`
	Metadata        map[string]string `json:"metadata"`
}

type actionEnvParams struct {
	agent   agentstore.Agent
	dir     string
	trigger string
	action  string
}

// Actions need the managed tmux socket because pane IDs are scoped to a tmux server.
func (s *ActionService) env(ctx context.Context, p actionEnvParams) ([]string, error) {
	agent, dir, trigger := p.agent, p.dir, p.trigger

	workflow, err := s.workflows.GetIncludingArchived(ctx, agent.WorkflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to read workflow %q: %w", agent.WorkflowID, err)
	}

	payload := actionPayload{
		ID:        agent.ID,
		Name:      agent.Name,
		State:     agent.State,
		Alive:     isPaneOwnedBy(s.tmux.GetPaneOwner(agent.Pane), agent.ID),
		Cwd:       agent.Cwd,
		Harness:   agent.Harness,
		SessionID: agent.SessionID,
		Flags:     agent.Flags,
		Pane:      agent.Pane,
		Workflow:  workflowName(workflow),
		Hidden:    agent.Hidden,
		Metadata:  make(map[string]string, len(agent.Metadata)),
	}
	payload.Prompt, payload.PromptTruncated = head(agent.Prompt, maxActionPrompt)
	if agent.Tag != nil {
		payload.Tag = *agent.Tag
	}
	if !agent.CreatedAt.IsZero() {
		payload.CreatedAt = agent.CreatedAt.Format(time.RFC3339)
	}
	for _, entry := range agent.Metadata {
		payload.Metadata[entry.Key] = entry.Value
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode agent %q: %w", agent.ID, err)
	}

	env := []string{
		"WEAVE_AGENT=" + string(encoded),
		"WEAVE_AGENT_ID=" + agent.ID,
		"WEAVE_AGENT_CWD=" + agent.Cwd,
		"WEAVE_ADDR=" + s.addr,
		"WEAVE_TMUX_SOCKET=" + s.socket,
		"WEAVE_ACTIONS_DIR=" + dir,
		"WEAVE_TRIGGER=" + trigger,
		"WEAVE_ACTION=" + p.action,
		"WEAVE_INFO_PANE=" + s.tmux.GetInfoPane(),
		"WEAVE_HOST_TMUX_SOCKET=" + s.tmux.GetHostSocket(),
		"WEAVE_HOST_CLIENT=" + s.tmux.ShowGlobal(tmux.InvokingClientOption),
		"EDITOR=" + s.editor,
		"WEAVE_SHELL=" + s.tmux.DefaultShell(),
	}

	if s.binPath != "" {
		existing, _ := config.LookupEnv("PATH")
		env = append(env, "PATH="+filepath.Dir(s.binPath)+string(os.PathListSeparator)+existing)
	}

	if workflow != nil && workflow.Worktree.Enabled {
		worktreeEnv, err := s.worktreeEnv(*workflow)
		if err != nil {
			return nil, err
		}

		env = append(env, worktreeEnv...)
	}

	return env, nil
}

func workflowName(workflow *workflowstore.Workflow) string {
	if workflow == nil {
		return ""
	}

	return workflow.Name
}

// worktreeEnv is what the built-in worktree setup and cleanup actions read their config
// from, since a workflow's worktree settings live in the database rather than in the
// script itself. The repo the worktrees are added off is named outright rather than left
// to WEAVE_AGENT_CWD, which stops being the repo the moment setup replaces it with the
// worktree and so would be wrong every time setup runs again on a resume. Cleanup asks for
// none of this, since a worktree's own git-common-dir names the repo it came from.
func (s *ActionService) worktreeEnv(workflow workflowstore.Workflow) ([]string, error) {
	loaded, err := s.load()
	if err != nil {
		return nil, fmt.Errorf("failed to read the config: %w", err)
	}

	worktreesDir, err := loaded.GetWorktreesDir()
	if err != nil {
		return nil, err
	}

	cfg := workflow.Worktree

	return []string{
		"WEAVE_WORKTREE_REPO=" + config.ExpandHome(strings.TrimSpace(workflow.Cwd)),
		"WEAVE_WORKTREES_DIR=" + worktreesDir,
		"WEAVE_WORKTREE_BASE_BRANCH=" + cfg.BaseBranch,
		"WEAVE_WORKTREE_COPY_PATHS=" + strings.Join(cfg.CopyPaths, "\n"),
		"WEAVE_WORKTREE_SYMLINK_PATHS=" + strings.Join(cfg.SymlinkPaths, "\n"),
	}, nil
}

// Avoid returning invalid UTF-8 when the byte limit splits a rune.
func head(s string, limit int) (string, bool) {
	if len(s) <= limit {
		return s, false
	}

	cut := s[:limit]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}

	return cut, true
}
