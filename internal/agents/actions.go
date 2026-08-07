package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"weave/config"
	"weave/internal/actions"
	"weave/internal/logging"
	"weave/internal/tmux"

	agentstore "weave/internal/store/agent"
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
	IsPaneAlive(pane string) bool
	RespawnPane(pane, dir string, argv ...string)
	WaitFor(ctx context.Context, channel string) error
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

// RunSetupAction runs a setup action before creating an agent.
func (s *ActionService) RunSetupAction(ctx context.Context, agent agentstore.Agent, name, pane string) (string, error) {
	return s.runAwaitedAction(ctx, prepareParams{agent: agent, name: name, trigger: TriggerSetup}, pane)
}

// RunResumeAction reruns a setup action before resuming an agent.
func (s *ActionService) RunResumeAction(ctx context.Context, agent agentstore.Agent, name string) (string, error) {
	return s.runAwaitedAction(ctx, prepareParams{agent: agent, name: name, trigger: TriggerResume}, "")
}

func (s *ActionService) runAwaitedAction(ctx context.Context, p prepareParams, pane string) (string, error) {
	if pane != "" && pane != s.tmux.GetInfoPane() {
		pane = ""
	}

	action, env, err := s.prepare(ctx, p)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrSetupFailed, err)
	}

	logger := s.actionLogger(ctx, p.name, p.trigger, p.agent)
	logger.LogAttrs(ctx, slog.LevelInfo, "action started")
	began := time.Now()

	timeout := action.Header.TimeoutOr(setupTimeout)

	var result actions.Result
	if pane != "" {
		result, err = s.runInPane(ctx, action, env, pane, timeout)
	} else {
		result, err = s.runner.RunAndWait(ctx, actions.RunParams{
			Action:  action,
			Dir:     action.GetDir(),
			Env:     env,
			Timeout: timeout,
		})
	}

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

const setupPaneOutputLimit = 64 << 10

const setupPaneWrapper = `
mkfifo "$7"
tee "$6" >&2 <"$7" &
teepid=$!
"$1" >"$4" 2>"$7"
ec=$?
wait "$teepid"
rm -f "$7"
echo "$ec" >"$5"
tmux -L "$2" wait-for -S "$3"
if [ "$ec" -ne 0 ]; then
  printf '\n[setup action exited %s -- press enter to close]\n' "$ec"
  read -r _dismiss
fi
`

const setupPaneGaveUpWrapper = `
printf '\n[setup action %s: gave up after %s]\n' "$1" "$2"
exec "$3"
`

func (s *ActionService) runInPane(ctx context.Context, action actions.Action, env []string, pane string, timeout time.Duration) (actions.Result, error) {
	if !action.Executable {
		return actions.Result{}, fmt.Errorf("failed to run action %s: %w", action.Name, ErrActionNotExecutable)
	}

	dir := action.GetDir()
	if info, statErr := os.Stat(dir); statErr != nil || !info.IsDir() {
		return actions.Result{}, fmt.Errorf("failed to run action %s in %s: %w", action.Name, dir, ErrActionDirMissing)
	}

	if !s.tmux.IsPaneAlive(pane) {
		return actions.Result{}, fmt.Errorf("failed to run action %s: %w: the pane %s is gone", action.Name, actions.ErrNotStartable, pane)
	}

	outFile, err := os.CreateTemp("", "weave-setup-out-")
	if err != nil {
		return actions.Result{}, fmt.Errorf("failed to run action %s: %w", action.Name, err)
	}
	outFile.Close()
	defer removeTemp(ctx, outFile.Name())

	exitFile, err := os.CreateTemp("", "weave-setup-exit-")
	if err != nil {
		return actions.Result{}, fmt.Errorf("failed to run action %s: %w", action.Name, err)
	}
	exitFile.Close()
	defer removeTemp(ctx, exitFile.Name())

	errFile, err := os.CreateTemp("", "weave-setup-err-")
	if err != nil {
		return actions.Result{}, fmt.Errorf("failed to run action %s: %w", action.Name, err)
	}
	errFile.Close()
	defer removeTemp(ctx, errFile.Name())

	fifoFile, err := os.CreateTemp("", "weave-setup-fifo-")
	if err != nil {
		return actions.Result{}, fmt.Errorf("failed to run action %s: %w", action.Name, err)
	}
	fifoPath := fifoFile.Name()
	fifoFile.Close()
	removeTemp(ctx, fifoPath)
	defer removeTemp(ctx, fifoPath)

	channel := fmt.Sprintf("weave-setup-%s-%d", strings.TrimPrefix(pane, "%"), time.Now().UnixNano())

	waitCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		waitCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	argv := append([]string{"env", "-i"}, os.Environ()...)
	argv = append(argv, env...)
	argv = append(argv, "sh", "-c", setupPaneWrapper, "sh",
		action.Path, s.socket, channel, outFile.Name(), exitFile.Name(), errFile.Name(), fifoPath)
	s.tmux.RespawnPane(pane, dir, argv...)

	waitErr := s.tmux.WaitFor(waitCtx, channel)

	stdout, err := os.ReadFile(outFile.Name())
	if err != nil {
		return actions.Result{}, fmt.Errorf("failed to run action %s: %w", action.Name, err)
	}
	stdout = tailBytes(stdout, setupPaneOutputLimit)

	stderr, readErr := os.ReadFile(errFile.Name())
	if readErr != nil {
		logging.From(ctx).Warn("failed to read setup pane stderr", "path", errFile.Name(), "error", readErr)
	}
	stderr = tailBytes(stderr, setupPaneOutputLimit)

	if waitErr != nil {
		if waitCtx.Err() != nil {
			shell := s.tmux.DefaultShell()
			if shell == "" {
				shell = "/bin/sh"
			}
			s.tmux.RespawnPane(pane, dir, "sh", "-c", setupPaneGaveUpWrapper, "sh", action.Name, timeout.String(), shell)

			return actions.Result{Stdout: stdout, Stderr: stderr, ExitCode: -1},
				fmt.Errorf("failed to run action %s: %w: gave up after %s", action.Name, waitCtx.Err(), timeout)
		}

		return actions.Result{Stdout: stdout, Stderr: stderr, ExitCode: -1},
			fmt.Errorf("failed to run action %s: could not tell whether it finished: %w", action.Name, waitErr)
	}

	exitRaw, err := os.ReadFile(exitFile.Name())
	if err != nil {
		return actions.Result{Stdout: stdout, Stderr: stderr, ExitCode: -1},
			fmt.Errorf("failed to run action %s: could not read its exit status: %w", action.Name, err)
	}

	exitCode, convErr := strconv.Atoi(strings.TrimSpace(string(exitRaw)))
	if convErr != nil {
		return actions.Result{Stdout: stdout, Stderr: stderr, ExitCode: -1},
			fmt.Errorf("failed to run action %s: could not read its exit status: %w", action.Name, convErr)
	}

	result := actions.Result{Stdout: stdout, Stderr: stderr, ExitCode: exitCode}
	if exitCode != 0 {
		return result, fmt.Errorf("failed to run action %s: %w: exited %d", action.Name, actions.ErrFailed, exitCode)
	}

	return result, nil
}

func removeTemp(ctx context.Context, path string) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		logging.From(ctx).Warn("failed to remove temp file", "path", path, "error", err)
	}
}

func tailBytes(b []byte, limit int) []byte {
	if len(b) <= limit {
		return b
	}

	return b[len(b)-limit:]
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

	return env, nil
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
