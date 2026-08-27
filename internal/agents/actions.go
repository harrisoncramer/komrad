package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
	TriggerManual = "manual"

	TriggerSetup = "setup"

	TriggerResume = "resume"

	TriggerCleanup = "cleanup"
)

type ActionService struct {
	agents     agentGetter
	workflows  workflowReader
	tmux       tmuxReader
	runner     actionRunner
	load       configLoader
	addr       string
	socket     string
	builtinDir string
	logsDir    string
	editor     string
	binPath    string

	catalog *actions.Catalog

	inFlightMu sync.Mutex
	inFlight   map[actionKey]struct{}
}

type ActionParams struct {
	Agents     agentGetter
	Workflows  workflowReader
	Tmux       tmuxReader
	Runner     actionRunner
	Load       configLoader
	Addr       string
	Socket     string
	BuiltinDir string
	LogsDir    string
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
		logsDir:    p.LogsDir,
		editor:     p.Editor,
		binPath:    p.BinPath,
		catalog:    actions.NewCatalog(),
		inFlight:   make(map[actionKey]struct{}),
	}
}

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

var ErrSetupFailed = errors.New("the setup action failed")

const setupTimeout = 10 * time.Minute

const cleanupTimeout = 2 * time.Minute

const maxSetupDir = 4096

const actionDetailCap = 400

const setupDirFileVar = "WEAVE_SETUP_DIR_FILE"

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

	timeout := action.Header.TimeoutOr(setupTimeout)
	logPath := s.actionLogPath(p.agent.ID, config.ActionLogSetup)

	var result actions.Result
	if pane != "" {
		result, err = s.runInPane(ctx, action, env, pane, logPath, timeout)
	} else {
		result, err = s.runner.RunAndWait(ctx, actions.RunParams{
			Action:  action,
			Dir:     action.GetDir(),
			Env:     env,
			Timeout: timeout,
		})
		writeActionLog(ctx, logPath, combinedOutput(result.Stdout, result.Stderr))
	}

	logger.LogAttrs(ctx, slog.LevelInfo, "action finished", finishedAction(result, began)...)

	if err != nil {
		return "", fmt.Errorf("%w: %w%s", ErrSetupFailed, err, actionDetail(result.Stdout, result.Stderr, logPath))
	}

	named, err := readSetupDir(dirPath)
	if err != nil {
		return "", fmt.Errorf("%w: failed to read the directory %s named: %w", ErrSetupFailed, p.name, err)
	}

	if len(named) > maxSetupDir {
		return "", fmt.Errorf("%w: %s wrote more than %d bytes to %s, where a directory to run in was expected", ErrSetupFailed, p.name, maxSetupDir, setupDirFileVar)
	}

	return string(bytes.TrimSpace(named)), nil
}

const setupPaneOutputLimit = 64 << 10

const setupPaneWrapper = `
mkfifo "$6"
tee "$5" >&2 <"$6" &
teepid=$!
"$1" >"$6" 2>&1
ec=$?
wait "$teepid"
rm -f "$6"
echo "$ec" >"$4"
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

var errPaneDied = errors.New("the pane closed before it finished")

const panePollInterval = 500 * time.Millisecond

func (s *ActionService) waitForPane(ctx context.Context, pane, channel string) error {
	done := make(chan error, 1)
	go func() { done <- s.tmux.WaitFor(ctx, channel) }()

	ticker := time.NewTicker(panePollInterval)
	defer ticker.Stop()

	for {
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			select {
			case err := <-done:
				return err
			default:
			}

			if !s.tmux.IsPaneAlive(pane) {
				return errPaneDied
			}
		}
	}
}

func (s *ActionService) runInPane(ctx context.Context, action actions.Action, env []string, pane, logPath string, timeout time.Duration) (actions.Result, error) {
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

	exitFile, err := os.CreateTemp("", "weave-setup-exit-")
	if err != nil {
		return actions.Result{}, fmt.Errorf("failed to run action %s: %w", action.Name, err)
	}
	exitFile.Close()
	defer removeTemp(ctx, exitFile.Name())

	errPath := logPath
	if errPath == "" {
		errFile, err := os.CreateTemp("", "weave-setup-err-")
		if err != nil {
			return actions.Result{}, fmt.Errorf("failed to run action %s: %w", action.Name, err)
		}
		errFile.Close()
		errPath = errFile.Name()
		defer removeTemp(ctx, errPath)
	}

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
		action.Path, s.socket, channel, exitFile.Name(), errPath, fifoPath)
	s.tmux.RespawnPane(pane, dir, argv...)

	waitErr := s.waitForPane(waitCtx, pane, channel)

	stderr, readErr := os.ReadFile(errPath)
	if readErr != nil {
		logging.From(ctx).Warn("failed to read setup pane stderr", "path", errPath, "error", readErr)
	}
	stderr = tailBytes(stderr, setupPaneOutputLimit)

	if waitErr != nil {
		if errors.Is(waitErr, errPaneDied) {
			stderr = appendNote(ctx, errPath, stderr, "the pane closed before the action finished")

			return actions.Result{Stderr: stderr, ExitCode: -1},
				fmt.Errorf("failed to run action %s: %w: %w", action.Name, actions.ErrNotStartable, errPaneDied)
		}

		if waitCtx.Err() != nil {
			shell := s.tmux.DefaultShell()
			if shell == "" {
				shell = "/bin/sh"
			}
			s.tmux.RespawnPane(pane, dir, "sh", "-c", setupPaneGaveUpWrapper, "sh", action.Name, timeout.String(), shell)
			stderr = appendNote(ctx, errPath, stderr, fmt.Sprintf("gave up after %s", timeout))

			return actions.Result{Stderr: stderr, ExitCode: -1},
				fmt.Errorf("failed to run action %s: %w: gave up after %s", action.Name, waitCtx.Err(), timeout)
		}

		stderr = appendNote(ctx, errPath, stderr, "could not tell whether it finished: "+waitErr.Error())

		return actions.Result{Stderr: stderr, ExitCode: -1},
			fmt.Errorf("failed to run action %s: could not tell whether it finished: %w", action.Name, waitErr)
	}

	exitRaw, err := os.ReadFile(exitFile.Name())
	if err != nil {
		return actions.Result{Stderr: stderr, ExitCode: -1},
			fmt.Errorf("failed to run action %s: could not read its exit status: %w", action.Name, err)
	}

	exitCode, convErr := strconv.Atoi(strings.TrimSpace(string(exitRaw)))
	if convErr != nil {
		return actions.Result{Stderr: stderr, ExitCode: -1},
			fmt.Errorf("failed to run action %s: could not read its exit status: %w", action.Name, convErr)
	}

	result := actions.Result{Stderr: stderr, ExitCode: exitCode}
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

func (s *ActionService) actionLogPath(agentID, kind string) string {
	if s.logsDir == "" || agentID == "" {
		return ""
	}

	return config.ActionLogPath(s.logsDir, agentID, kind)
}

func appendNote(ctx context.Context, path string, stderr []byte, note string) []byte {
	line := []byte("[" + note + "]\n")
	if len(stderr) > 0 && stderr[len(stderr)-1] != '\n' {
		line = append([]byte("\n"), line...)
	}

	if path != "" {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			logging.From(ctx).Warn("failed to append to action log", "path", path, "error", err)
		} else {
			if _, err := f.Write(line); err != nil {
				logging.From(ctx).Warn("failed to append to action log", "path", path, "error", err)
			}
			f.Close()
		}
	}

	return tailBytes(append(stderr, line...), setupPaneOutputLimit)
}

func writeActionLog(ctx context.Context, path string, content []byte) {
	if path == "" {
		return
	}

	if err := os.WriteFile(path, content, 0o644); err != nil {
		logging.From(ctx).Warn("failed to write action log", "path", path, "error", err)
	}
}

func readSetupDir(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return io.ReadAll(io.LimitReader(file, maxSetupDir+1))
}

// RunCleanupAction starts a cleanup action without waiting for it to finish. done, when
// given, fires exactly once with the action's outcome once it actually finishes — nil for
// success, the failure otherwise — but never for a refusal to start, which the return error
// already reports.
func (s *ActionService) RunCleanupAction(ctx context.Context, agent agentstore.Agent, name string, done func(error)) error {
	action, env, err := s.prepare(ctx, prepareParams{agent: agent, name: name, trigger: TriggerCleanup})
	if err != nil {
		return err
	}

	return s.start(ctx, startParams{
		action:  action,
		env:     env,
		agent:   agent,
		trigger: TriggerCleanup,
		logPath: s.actionLogPath(agent.ID, config.ActionLogTeardown),
		done:    done,
	})
}

var ErrActionAlreadyRunning = errors.New("the action is already running for this agent")

type actionKey struct {
	agentID string
	name    string
}

func (s *ActionService) claim(key actionKey) bool {
	s.inFlightMu.Lock()
	defer s.inFlightMu.Unlock()

	if _, running := s.inFlight[key]; running {
		return false
	}

	s.inFlight[key] = struct{}{}

	return true
}

func (s *ActionService) release(key actionKey) {
	s.inFlightMu.Lock()
	defer s.inFlightMu.Unlock()

	delete(s.inFlight, key)
}

type startParams struct {
	action  actions.Action
	env     []string
	agent   agentstore.Agent
	trigger string
	logPath string
	done    func(error)
}

func timeoutFallback(trigger string) time.Duration {
	if trigger == TriggerCleanup {
		return cleanupTimeout
	}

	return 0
}

func (s *ActionService) start(ctx context.Context, p startParams) error {
	key := actionKey{agentID: p.agent.ID, name: p.action.Name}
	if !s.claim(key) {
		return fmt.Errorf("failed to run action %s: %w", p.action.Name, ErrActionAlreadyRunning)
	}

	logger := s.actionLogger(ctx, p.action.Name, p.trigger, p.agent)
	ctx = context.WithoutCancel(ctx)
	began := time.Now()

	err := s.runner.Run(actions.RunParams{
		Action: p.action,
		Dir:    p.action.GetDir(),
		Env:    p.env,

		Timeout: p.action.Header.TimeoutOr(timeoutFallback(p.trigger)),
		Done: func(result actions.Result, err error) {
			defer s.release(key)

			writeActionLog(ctx, p.logPath, combinedOutput(result.Stdout, result.Stderr))

			if err != nil {
				logger.LogAttrs(ctx, slog.LevelError, "action failed",
					append(failedAction(result, err), slog.Int64(logging.DurationMS, elapsedMS(began)))...)
			} else {
				logger.LogAttrs(ctx, slog.LevelInfo, "action finished", finishedAction(result, began)...)
			}

			if p.done != nil {
				p.done(err)
			}
		},
	})
	if err != nil {
		s.release(key)

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

func finishedAction(result actions.Result, began time.Time) []slog.Attr {
	attrs := []slog.Attr{
		slog.Int(logging.ExitCode, result.ExitCode),
		slog.Int64(logging.DurationMS, elapsedMS(began)),
	}

	return append(attrs, outputAttrs(result)...)
}

func failedAction(result actions.Result, err error) []slog.Attr {
	attrs := []slog.Attr{slog.Int(logging.ExitCode, result.ExitCode)}
	attrs = append(attrs, logging.Err(err)...)

	return append(attrs, outputAttrs(result)...)
}

func outputAttrs(result actions.Result) []slog.Attr {
	var attrs []slog.Attr

	if said := bytes.TrimSpace(result.Stdout); len(said) > 0 {
		attrs = append(attrs, slog.String(logging.ActionStdout, string(said)))
	}
	if said := bytes.TrimSpace(result.Stderr); len(said) > 0 {
		attrs = append(attrs, slog.String(logging.ActionStderr, string(said)))
	}

	return attrs
}

func combinedOutput(stdout, stderr []byte) []byte {
	if len(stdout) == 0 {
		return stderr
	}
	if len(stderr) == 0 {
		return stdout
	}

	return bytes.Join([][]byte{stdout, stderr}, []byte("\n"))
}

func actionDetail(stdout, stderr []byte, logPath string) string {
	stdout = bytes.TrimSpace(stdout)
	stderr = bytes.TrimSpace(stderr)
	total := len(stdout) + len(stderr)
	if total == 0 {
		return ""
	}
	if total <= actionDetailCap {
		return ": " + string(combinedOutput(stdout, stderr))
	}

	keptStderr := tailBytes(stderr, actionDetailCap)
	keptStdout := tailBytes(stdout, actionDetailCap-len(keptStderr))
	kept := string(combinedOutput(keptStdout, keptStderr))
	cut := total - len(keptStdout) - len(keptStderr)

	if logPath == "" {
		return fmt.Sprintf(": ...%s (%d earlier bytes cut, the rest was not kept)", kept, cut)
	}

	return fmt.Sprintf(": ...%s (%d earlier bytes cut, kept in %s)", kept, cut, logPath)
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

	env = append(env, gitPromptGuardEnv()...)

	if workflow != nil && workflow.Worktree.Enabled {
		worktreeEnv, err := s.worktreeEnv(*workflow, agent)
		if err != nil {
			return nil, err
		}

		env = append(env, worktreeEnv...)
	}

	return env, nil
}

func gitPromptGuardEnv() []string {
	guard := []string{
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=never",
	}

	if _, set := config.LookupEnv("GIT_ASKPASS"); !set {
		guard = append(guard, "GIT_ASKPASS=")
	}
	if _, set := config.LookupEnv("SSH_ASKPASS"); !set {
		guard = append(guard, "SSH_ASKPASS=")
	}

	return guard
}

func workflowName(workflow *workflowstore.Workflow) string {
	if workflow == nil {
		return ""
	}

	return workflow.Name
}

func (s *ActionService) worktreeEnv(workflow workflowstore.Workflow, agent agentstore.Agent) ([]string, error) {
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
		"WEAVE_WORKTREE_ADOPT_BRANCH=" + agent.AdoptedBranch,
	}, nil
}

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
