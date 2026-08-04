package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"weave/config"
	"weave/internal/actions"
	"weave/internal/logging"
	"weave/internal/tmux"
	"weave/lib/xslice"

	agentstore "weave/internal/store/agent"
)

type agentGetter interface {
	GetByID(ctx context.Context, id string) (*agentstore.Agent, error)
}

type tmuxReader interface {
	GetPaneOwner(pane string) string
	GetHostSocket() string
	GetPanelGlobal(name string) string
	ShowGlobal(name string) string
	DefaultShell() string
}

type actionRunner interface {
	Discover(dir string) ([]actions.Action, error)
	Run(p actions.RunParams) error
	RunAndWait(ctx context.Context, p actions.RunParams) (actions.Result, error)
}

type configLoader func() (*config.Config, error)

// ErrActionNotFound reports that the actions directory holds no file by that name.
var ErrActionNotFound = errors.New("action not found")

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
	workflows  storedWorkflowReader
	tmux       tmuxReader
	runner     actionRunner
	load       configLoader
	addr       string
	socket     string
	builtinDir string
	editor     string

	scanMu    sync.Mutex
	scanCache map[string]dirScan
}

// ActionParams carries the action service's dependencies.
type ActionParams struct {
	Agents     agentGetter
	Workflows  storedWorkflowReader
	Tmux       tmuxReader
	Runner     actionRunner
	Load       configLoader
	Addr       string
	Socket     string
	BuiltinDir string
	Editor     string
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
	}
}

// Exec arguments and environment variables share a kernel size limit.
const maxActionPrompt = 16 * 1024

// ListActions returns the available actions in name order.
func (s *ActionService) ListActions() ([]actions.Action, error) {
	_, found, err := s.discover()
	if err != nil {
		return nil, err
	}

	return xslice.Map(found, func(action foundAction) actions.Action { return action.Action }), nil
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

	prepared, err := s.prepare(ctx, prepareParams{agent: *agent, name: name, trigger: TriggerManual})
	if err != nil {
		return err
	}

	return s.start(ctx, startParams{
		action:  prepared.action,
		env:     prepared.env,
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

	prepared, err := s.prepare(ctx, prepareParams{agent: *agent, name: editActionName, trigger: TriggerManual})
	if err != nil {
		return err
	}

	return s.start(ctx, startParams{
		action:  prepared.action,
		env:     append(prepared.env, editPathVar+"="+target.Path),
		agent:   *agent,
		trigger: TriggerManual,
	})
}

// ErrSetupFailed reports that an agent setup action failed.
var ErrSetupFailed = errors.New("the setup action failed")

const setupTimeout = 10 * time.Minute

const maxSetupDir = 4096

// RunSetupAction runs a setup action before creating an agent.
func (s *ActionService) RunSetupAction(ctx context.Context, agent agentstore.Agent, name string) (string, error) {
	return s.runAwaitedAction(ctx, prepareParams{agent: agent, name: name, trigger: TriggerSetup})
}

// RunResumeAction reruns a setup action before resuming an agent.
func (s *ActionService) RunResumeAction(ctx context.Context, agent agentstore.Agent, name string) (string, error) {
	return s.runAwaitedAction(ctx, prepareParams{agent: agent, name: name, trigger: TriggerResume})
}

func (s *ActionService) runAwaitedAction(ctx context.Context, p prepareParams) (string, error) {
	prepared, err := s.prepare(ctx, p)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrSetupFailed, err)
	}
	action, env := prepared.action, prepared.env

	logger := s.actionLogger(ctx, actionRecord{name: p.name, trigger: p.trigger, agent: p.agent})
	logger.LogAttrs(ctx, slog.LevelInfo, "action started")
	began := time.Now()

	result, err := s.runner.RunAndWait(ctx, actions.RunParams{
		Action:  action.Action,
		Dir:     action.dir,
		Env:     env,
		Timeout: timeoutFor(action, setupTimeout),
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

	answered := string(bytes.TrimSpace(result.Stdout))
	if len(answered) > maxSetupDir {
		return "", fmt.Errorf("%w: %s wrote %d bytes to stdout, where a directory to run in was expected. Progress belongs on stderr", ErrSetupFailed, p.name, len(answered))
	}

	return answered, nil
}

// RunCleanupAction starts a cleanup action without waiting for it to finish.
func (s *ActionService) RunCleanupAction(ctx context.Context, agent agentstore.Agent, name string) error {
	prepared, err := s.prepare(ctx, prepareParams{agent: agent, name: name, trigger: TriggerCleanup})
	if err != nil {
		return err
	}

	return s.start(ctx, startParams{
		action:  prepared.action,
		env:     prepared.env,
		agent:   agent,
		trigger: TriggerCleanup,
	})
}

type startParams struct {
	action  foundAction
	env     []string
	agent   agentstore.Agent
	trigger string
}

func (s *ActionService) start(ctx context.Context, p startParams) error {
	logger := s.actionLogger(ctx, actionRecord{name: p.action.Name, trigger: p.trigger, agent: p.agent})
	ctx = context.WithoutCancel(ctx)
	began := time.Now()

	err := s.runner.Run(actions.RunParams{
		Action: p.action.Action,
		Dir:    p.action.dir,
		Env:    p.env,
		// Nothing is waiting, so an action that declared no timeout runs as long as it likes.
		Timeout: timeoutFor(p.action, 0),
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

type actionRecord struct {
	name    string
	trigger string
	agent   agentstore.Agent
}

func (s *ActionService) actionLogger(ctx context.Context, r actionRecord) *slog.Logger {
	return logging.From(ctx).With(logging.Attrs([]slog.Attr{
		slog.String(logging.ActionName, r.name),
		slog.String(logging.ActionTrigger, r.trigger),
		slog.String(logging.ActionAgentID, r.agent.ID),
		slog.String(logging.ActionAgentCwd, r.agent.Cwd),
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

type foundAction struct {
	actions.Action
	dir string
}

// preparedAction is an action together with the environment a run of it needs, the two
// things every caller works out before it can start one.
type preparedAction struct {
	action foundAction
	env    []string
}

type prepareParams struct {
	agent   agentstore.Agent
	name    string
	trigger string
}

// prepare finds the named action and builds the environment a run of it is handed. Every
// caller does both before starting anything, so this is where that preparation happens,
// leaving the actual start of the process to whichever of start or RunAndWait the caller
// needs.
func (s *ActionService) prepare(ctx context.Context, p prepareParams) (preparedAction, error) {
	action, err := s.find(p.name)
	if err != nil {
		return preparedAction{}, err
	}

	env, err := s.env(ctx, actionEnvParams{agent: p.agent, dir: action.dir, trigger: p.trigger})
	if err != nil {
		return preparedAction{}, err
	}

	return preparedAction{action: action, env: env}, nil
}

// Match discovered names instead of joining untrusted input onto a path.
func (s *ActionService) find(name string) (foundAction, error) {
	dir, found, err := s.discover()
	if err != nil {
		return foundAction{}, err
	}

	for _, action := range found {
		if action.Name == name {
			return action, nil
		}
	}

	return foundAction{}, fmt.Errorf("%w: %q is not in %s", ErrActionNotFound, name, dir)
}

func timeoutFor(action foundAction, fallback time.Duration) time.Duration {
	if action.Header.Timeout > 0 {
		return action.Header.Timeout
	}

	return fallback
}

// Reload config so action directory changes take effect without a restart.
func (s *ActionService) discover() (string, []foundAction, error) {
	cfg, err := s.load()
	if err != nil {
		return "", nil, fmt.Errorf("failed to read the config: %w", err)
	}

	userDir, err := cfg.GetActionsDir()
	if err != nil {
		return "", nil, err
	}

	var found []foundAction
	claimed := make(map[string]bool)

	for _, dir := range []string{userDir, s.builtinDir} {
		if dir == "" {
			continue
		}

		listed, err := s.scan(dir)
		if err != nil {
			return "", nil, err
		}

		for _, action := range listed {
			if claimed[action.Name] {
				continue
			}

			claimed[action.Name] = true
			found = append(found, foundAction{Action: action, dir: dir})
		}
	}

	slices.SortFunc(found, func(a, b foundAction) int { return strings.Compare(a.Name, b.Name) })

	return userDir, found, nil
}

// dirScan is what scan last found in a directory, kept beside the directory's own mtime so
// a later call can tell whether anything has been added or removed since.
type dirScan struct {
	modTime time.Time
	actions []actions.Action
}

// scan lists the actions in dir, reusing what an earlier call found there as long as the
// directory's own mtime has not moved. Every action invocation works through here, and
// each entry's header is read off disk, so rescanning on every one of them would mean
// re-reading every action's file to run any single one. A directory gains or loses an mtime
// bump only when a file is added, removed, or renamed inside it, which is the change this
// is meant to catch; a script edited in place keeps its old mtime and is picked up the next
// time something else in the directory changes.
func (s *ActionService) scan(dir string) ([]actions.Action, error) {
	modTime, err := dirModTime(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to stat the actions directory %s: %w", dir, err)
	}

	s.scanMu.Lock()
	cached, ok := s.scanCache[dir]
	s.scanMu.Unlock()
	if ok && cached.modTime.Equal(modTime) {
		return cached.actions, nil
	}

	listed, err := s.runner.Discover(dir)
	if err != nil {
		return nil, err
	}

	s.scanMu.Lock()
	if s.scanCache == nil {
		s.scanCache = make(map[string]dirScan)
	}
	s.scanCache[dir] = dirScan{modTime: modTime, actions: listed}
	s.scanMu.Unlock()

	return listed, nil
}

// dirModTime is a directory's own mtime, or the zero time for one that does not exist,
// which Discover treats the same way: as holding nothing rather than as an error.
func dirModTime(dir string) (time.Time, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, nil
		}

		return time.Time{}, err
	}

	return info.ModTime(), nil
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
	Mark            string            `json:"mark"`
	Hidden          bool              `json:"hidden"`
	CreatedAt       string            `json:"created_at"`
	Metadata        map[string]string `json:"metadata"`
}

type actionEnvParams struct {
	agent   agentstore.Agent
	dir     string
	trigger string
}

// Actions need the managed tmux socket because pane IDs are scoped to a tmux server.
func (s *ActionService) env(ctx context.Context, p actionEnvParams) ([]string, error) {
	agent, dir, trigger := p.agent, p.dir, p.trigger

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
		Workflow:  s.workflowName(ctx, agent.WorkflowID),
		Hidden:    agent.Hidden,
		Metadata:  make(map[string]string, len(agent.Metadata)),
	}
	payload.Prompt, payload.PromptTruncated = head(agent.Prompt, maxActionPrompt)
	if agent.Mark != nil {
		payload.Mark = *agent.Mark
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

	return []string{
		"WEAVE_AGENT=" + string(encoded),
		"WEAVE_AGENT_ID=" + agent.ID,
		"WEAVE_AGENT_CWD=" + agent.Cwd,
		"WEAVE_ADDR=" + s.addr,
		"WEAVE_TMUX_SOCKET=" + s.socket,
		"WEAVE_ACTIONS_DIR=" + dir,
		"WEAVE_TRIGGER=" + trigger,
		"WEAVE_SECONDARY_PANE=" + s.tmux.GetPanelGlobal(tmux.SecondaryPaneOption),
		"WEAVE_HOST_TMUX_SOCKET=" + s.tmux.GetHostSocket(),
		"WEAVE_HOST_CLIENT=" + s.tmux.ShowGlobal(tmux.InvokingClientOption),
		"EDITOR=" + s.editor,
		"WEAVE_SHELL=" + s.tmux.DefaultShell(),
	}, nil
}

func (s *ActionService) workflowName(ctx context.Context, id string) string {
	found, err := s.workflows.GetIncludingArchived(ctx, id)
	if err != nil || found == nil {
		return ""
	}

	return found.Name
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
