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
	"weave/internal/logging"
	"weave/internal/scripts"
	"weave/internal/store"
	"weave/internal/tmux"

	agentstore "weave/internal/store/agent"
	runstore "weave/internal/store/run"
	workflowstore "weave/internal/store/workflow"
)

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

type scriptRunner interface {
	Run(p scripts.RunParams) error
	RunAndWait(ctx context.Context, p scripts.RunParams) (scripts.Result, error)
}

type runRecorder interface {
	Begin(ctx context.Context, params runstore.BeginParams) (string, error)
	Finish(ctx context.Context, params runstore.FinishParams) error
	Abandon(ctx context.Context, id string) error
	Prune(ctx context.Context, agentID string, keep int) error
}

type configLoader func() (*config.Config, error)

const (
	TriggerOpenPR      = config.TriggerOpenPR
	TriggerProvision   = config.TriggerProvision
	TriggerResume      = config.TriggerResume
	TriggerDeprovision = config.TriggerDeprovision
)

type ScriptService struct {
	agents     agentGetter
	workflows  workflowReader
	tmux       tmuxReader
	runner     scriptRunner
	runs       runRecorder
	load       configLoader
	addr       string
	socket     string
	builtinDir string
	logsDir    string
	editor     string
	binPath    string
	catalog    *scripts.Catalog
	inFlightMu sync.Mutex
	inFlight   map[scriptKey]struct{}
	logRunMu   sync.Mutex
}

type ScriptParams struct {
	Agents     agentGetter
	Workflows  workflowReader
	Tmux       tmuxReader
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
		catalog:    scripts.NewCatalog(),
		inFlight:   make(map[scriptKey]struct{}),
	}
}

const maxScriptPrompt = 16 * 1024

// openPRScript is the built-in behind the panel's own key, and the one script weave runs
// because a person asked rather than because a moment came round.
const openPRScript = "_open_pr"

// OpenPullRequest starts the built-in script that opens an agent's recorded pull request,
// without waiting for the browser it asks.
func (s *ScriptService) OpenPullRequest(ctx context.Context, agentID string) error {
	agent, err := s.agents.GetByID(ctx, agentID)
	if errors.Is(err, store.ErrNotFound) {
		return ErrAgentNotFound
	}
	if err != nil {
		return fmt.Errorf("failed to read agent %q: %w", agentID, err)
	}

	script, env, err := s.prepare(ctx, prepareParams{
		agent:   *agent,
		name:    openPRScript,
		trigger: TriggerOpenPR,
	})
	if err != nil {
		return err
	}

	return s.start(ctx, startParams{
		script:  script,
		env:     env,
		agent:   *agent,
		trigger: TriggerOpenPR,
		run:     s.beginRun(ctx, agent.ID, TriggerOpenPR, openPRScript),
	})
}

var ErrProvisionFailed = errors.New("the provisioner failed")

const provisionTimeout = 10 * time.Minute

const deprovisionTimeout = 2 * time.Minute

const maxProvisionedDir = 4096

const scriptDetailCap = 400

const setupDirFileVar = "WEAVE_SETUP_DIR_FILE"

// RunProvision runs the provisioner that owns an agent's directory before creating it.
func (s *ScriptService) RunProvision(ctx context.Context, agent agentstore.Agent, name, pane string) (string, error) {
	return s.runAwaited(ctx, prepareParams{
		agent:   agent,
		name:    name,
		trigger: TriggerProvision,
	}, pane)
}

var ErrHookFailed = errors.New("a hook failed")

const hookTimeout = 10 * time.Minute

// ListHooks returns the available hooks in name order, which is what the workflow form offers
// and the order they run in on a moment that holds several.
func (s *ScriptService) ListHooks() ([]scripts.Script, error) {
	dirs, err := s.hookDirs()
	if err != nil {
		return nil, err
	}

	return s.catalog.List(dirs...)
}

// RunHook runs one of a workflow's hooks and waits for it, in the directory the agent was
// provisioned rather than the one the hook was found in, since a hook is there to work on the
// checkout.
func (s *ScriptService) RunHook(ctx context.Context, agent agentstore.Agent, name, event string) error {
	record := s.beginRun(ctx, agent.ID, event, name)

	script, env, err := s.prepare(ctx, prepareParams{
		agent:   agent,
		name:    name,
		trigger: event,
		hook:    true,
	})
	if err != nil {
		return s.refuseHook(ctx, record, name, err)
	}

	// A hook is there to work on the agent's own directory, so one that has gone is a refusal
	// rather than a reason to run the script somewhere else.
	if err := requireUsableDir(agent.Cwd); err != nil {
		return s.refuseHook(ctx, record, name, err)
	}

	logger := s.runLogger(ctx, name, event, agent)
	logger.LogAttrs(ctx, slog.LevelInfo, "script started")
	began := time.Now()

	result, err := s.runner.RunAndWait(ctx, scripts.RunParams{
		Script:  script,
		Dir:     agent.Cwd,
		Env:     env,
		Timeout: script.Header.TimeoutOr(hookTimeout),
	})
	writeRunLog(ctx, record.logPath, combinedOutput(result.Stdout, result.Stderr))

	if err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "script failed", failedScript(result, err)...)
		failed := fmt.Errorf("%w: %s: %w%s", ErrHookFailed, name, err, scriptDetail(result.Stdout, result.Stderr, record.logPath))
		noteEmptyLog(ctx, record.logPath, failed)
		s.finishRun(ctx, record, result, failed)

		return failed
	}

	s.finishRun(ctx, record, result, nil)
	logger.LogAttrs(ctx, slog.LevelInfo, "script finished", finishedScript(result, began)...)

	return nil
}

// refuseHook puts the reason a hook never started into the log the panel offers for it, since
// a run number holding nothing opens an empty pager on the one screen that was meant to say why.
func (s *ScriptService) refuseHook(ctx context.Context, record runRecord, name string, cause error) error {
	err := fmt.Errorf("%w: %s: %w", ErrHookFailed, name, cause)
	writeRunLog(ctx, record.logPath, []byte(err.Error()+"\n"))
	s.finishRun(ctx, record, scripts.Result{ExitCode: -1}, err)

	return err
}

// RunResume reruns the provisioner before resuming an agent.
func (s *ScriptService) RunResume(ctx context.Context, agent agentstore.Agent, name string) (string, error) {
	return s.runAwaited(ctx, prepareParams{
		agent:   agent,
		name:    name,
		trigger: TriggerResume,
	}, "")
}

func (s *ScriptService) runAwaited(ctx context.Context, p prepareParams, pane string) (string, error) {
	if pane != "" && pane != s.tmux.GetInfoPane() {
		pane = ""
	}

	script, env, err := s.prepare(ctx, p)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrProvisionFailed, err)
	}

	dirFile, err := os.CreateTemp("", "weave-provision-dir-*")
	if err != nil {
		return "", fmt.Errorf("%w: failed to prepare a place for %s to name its directory: %w", ErrProvisionFailed, p.name, err)
	}
	dirPath := dirFile.Name()
	closeErr := dirFile.Close()
	defer func() {
		if removeErr := os.Remove(dirPath); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			s.runLogger(ctx, p.name, p.trigger, p.agent).LogAttrs(ctx, slog.LevelWarn,
				"failed to remove the provisioner's directory file", logging.Err(removeErr)...)
		}
	}()
	if closeErr != nil {
		return "", fmt.Errorf("%w: failed to prepare a place for %s to name its directory: %w", ErrProvisionFailed, p.name, closeErr)
	}

	env = append(env, setupDirFileVar+"="+dirPath)

	logger := s.runLogger(ctx, p.name, p.trigger, p.agent)
	logger.LogAttrs(ctx, slog.LevelInfo, "script started")
	began := time.Now()

	timeout := script.Header.TimeoutOr(provisionTimeout)
	record := s.beginRun(ctx, p.agent.ID, p.trigger, p.name)

	var result scripts.Result
	if pane != "" {
		result, err = s.runInPane(ctx, script, env, pane, record.logPath, timeout)
	} else {
		result, err = s.runner.RunAndWait(ctx, scripts.RunParams{
			Script:  script,
			Dir:     script.GetDir(),
			Env:     env,
			Timeout: timeout,
		})
		writeRunLog(ctx, record.logPath, combinedOutput(result.Stdout, result.Stderr))
	}

	logger.LogAttrs(ctx, slog.LevelInfo, "script finished", finishedScript(result, began)...)

	if err != nil {
		failed := fmt.Errorf("%w: %w%s", ErrProvisionFailed, err, scriptDetail(result.Stdout, result.Stderr, record.logPath))
		noteEmptyLog(ctx, record.logPath, failed)
		s.finishRun(ctx, record, result, failed)

		return "", failed
	}

	s.finishRun(ctx, record, result, nil)

	named, err := readProvisionedDir(dirPath)
	if err != nil {
		return "", fmt.Errorf("%w: failed to read the directory %s named: %w", ErrProvisionFailed, p.name, err)
	}

	if len(named) > maxProvisionedDir {
		return "", fmt.Errorf("%w: %s wrote more than %d bytes to %s, where a directory to run in was expected", ErrProvisionFailed, p.name, maxProvisionedDir, setupDirFileVar)
	}

	return string(bytes.TrimSpace(named)), nil
}

const setupPaneOutputLimit = 64 << 10

// setupPaneWrapper prints what happened before it signals, so a caller woken by the signal
// looks at a pane that already says it.
const setupPaneWrapper = `
mkfifo "$6"
tee "$5" >&2 <"$6" &
teepid=$!
"$1" >"$6" 2>&1
ec=$?
wait "$teepid"
rm -f "$6"
echo "$ec" >"$4"
if [ "$ec" -ne 0 ]; then
  printf '\n[provisioner exited %s -- press enter to close]\n' "$ec"
fi
tmux -L "$2" wait-for -S "$3"
if [ "$ec" -ne 0 ]; then
  read -r _dismiss
fi
`

const setupPaneGaveUpWrapper = `
printf '\n[provisioner %s: gave up after %s]\n' "$1" "$2"
exec "$3"
`

var errPaneDied = errors.New("the pane closed before it finished")

const panePollInterval = 500 * time.Millisecond

func (s *ScriptService) waitForPane(ctx context.Context, pane, channel string) error {
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

func (s *ScriptService) runInPane(ctx context.Context, script scripts.Script, env []string, pane, logPath string, timeout time.Duration) (scripts.Result, error) {
	if !script.Executable {
		return scripts.Result{ExitCode: -1}, fmt.Errorf("failed to run script %s: %w", script.Name, ErrScriptNotExecutable)
	}

	dir := script.GetDir()
	if info, statErr := os.Stat(dir); statErr != nil || !info.IsDir() {
		return scripts.Result{ExitCode: -1}, fmt.Errorf("failed to run script %s in %s: %w", script.Name, dir, ErrScriptDirMissing)
	}

	if !s.tmux.IsPaneAlive(pane) {
		return scripts.Result{ExitCode: -1}, fmt.Errorf("failed to run script %s: %w: the pane %s is gone", script.Name, scripts.ErrNotStartable, pane)
	}

	exitFile, err := os.CreateTemp("", "weave-provision-exit-")
	if err != nil {
		return scripts.Result{ExitCode: -1}, fmt.Errorf("failed to run script %s: %w", script.Name, err)
	}
	exitFile.Close()
	defer removeTemp(ctx, exitFile.Name())

	errPath := logPath
	if errPath == "" {
		errFile, err := os.CreateTemp("", "weave-provision-err-")
		if err != nil {
			return scripts.Result{ExitCode: -1}, fmt.Errorf("failed to run script %s: %w", script.Name, err)
		}
		errFile.Close()
		errPath = errFile.Name()
		defer removeTemp(ctx, errPath)
	}

	fifoFile, err := os.CreateTemp("", "weave-provision-fifo-")
	if err != nil {
		return scripts.Result{ExitCode: -1}, fmt.Errorf("failed to run script %s: %w", script.Name, err)
	}
	fifoPath := fifoFile.Name()
	fifoFile.Close()
	removeTemp(ctx, fifoPath)
	defer removeTemp(ctx, fifoPath)

	channel := fmt.Sprintf("weave-provision-%s-%d", strings.TrimPrefix(pane, "%"), time.Now().UnixNano())

	waitCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		waitCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	argv := append([]string{"env", "-i"}, os.Environ()...)
	argv = append(argv, env...)
	argv = append(argv, "sh", "-c", setupPaneWrapper, "sh",
		script.Path, s.socket, channel, exitFile.Name(), errPath, fifoPath)
	s.tmux.RespawnPane(pane, dir, argv...)

	waitErr := s.waitForPane(waitCtx, pane, channel)

	stderr, readErr := os.ReadFile(errPath)
	if readErr != nil {
		logging.From(ctx).Warn("failed to read setup pane stderr", "path", errPath, "error", readErr)
	}
	stderr = tailBytes(stderr, setupPaneOutputLimit)

	if waitErr != nil {
		if errors.Is(waitErr, errPaneDied) {
			stderr = appendNote(ctx, errPath, stderr, "the pane closed before the script finished")

			return scripts.Result{Stderr: stderr, ExitCode: -1},
				fmt.Errorf("failed to run script %s: %w: %w", script.Name, scripts.ErrNotStartable, errPaneDied)
		}

		if waitCtx.Err() != nil {
			shell := s.tmux.DefaultShell()
			if shell == "" {
				shell = "/bin/sh"
			}
			s.tmux.RespawnPane(pane, dir, "sh", "-c", setupPaneGaveUpWrapper, "sh", script.Name, timeout.String(), shell)
			stderr = appendNote(ctx, errPath, stderr, fmt.Sprintf("gave up after %s", timeout))

			return scripts.Result{Stderr: stderr, ExitCode: -1},
				fmt.Errorf("failed to run script %s: %w: gave up after %s", script.Name, waitCtx.Err(), timeout)
		}

		stderr = appendNote(ctx, errPath, stderr, "could not tell whether it finished: "+waitErr.Error())

		return scripts.Result{Stderr: stderr, ExitCode: -1},
			fmt.Errorf("failed to run script %s: could not tell whether it finished: %w", script.Name, waitErr)
	}

	exitRaw, err := os.ReadFile(exitFile.Name())
	if err != nil {
		return scripts.Result{Stderr: stderr, ExitCode: -1},
			fmt.Errorf("failed to run script %s: could not read its exit status: %w", script.Name, err)
	}

	exitCode, convErr := strconv.Atoi(strings.TrimSpace(string(exitRaw)))
	if convErr != nil {
		return scripts.Result{Stderr: stderr, ExitCode: -1},
			fmt.Errorf("failed to run script %s: could not read its exit status: %w", script.Name, convErr)
	}

	result := scripts.Result{Stderr: stderr, ExitCode: exitCode}
	if exitCode != 0 {
		return result, fmt.Errorf("failed to run script %s: %w: exited %d", script.Name, scripts.ErrFailed, exitCode)
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

// maxLogRunAttempts bounds the walk up through numbers a second process has already taken,
// since a run whose log could not be named is still a run that has to happen.
const maxLogRunAttempts = 64

// claimRunLog names the file one run writes to and creates it empty, taking the number under
// the lock and by an exclusive create, since a run fired by hand and a launch can begin at the
// same moment and what the directory said an instant ago is not what it says now.
func (s *ScriptService) claimRunLog(ctx context.Context, agentID, trigger, name string) (string, int) {
	if s.logsDir == "" || agentID == "" {
		return "", 0
	}

	s.logRunMu.Lock()
	defer s.logRunMu.Unlock()

	dir := config.RunLogDir(s.logsDir, agentID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logging.From(ctx).Warn("failed to make an agent's run log directory", "path", dir, "error", err)

		return "", 0
	}

	number := 1
	if runs := config.ListRunLogs(s.logsDir, agentID); len(runs) > 0 {
		number = runs[0].Number + 1
	}

	for range maxLogRunAttempts {
		path := filepath.Join(dir, config.RunLogName(number, trigger, name))

		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if errors.Is(err, fs.ErrExist) {
			number++

			continue
		}
		if err != nil {
			logging.From(ctx).Warn("failed to open a run log", "path", path, "error", err)

			return "", 0
		}
		if closeErr := file.Close(); closeErr != nil {
			logging.From(ctx).Warn("failed to close a run log", "path", path, "error", closeErr)
		}

		pruneRunLogs(ctx, s.logsDir, agentID)

		return path, number
	}

	logging.From(ctx).Warn("failed to name a run of a script", "agent_id", agentID, "script", name)

	return "", 0
}

// runRecord is one run as weave is watching it.
type runRecord struct {
	logPath string
	id      string
}

// beginRun claims a run's log file and opens the row that indexes it, so a script that fails/succeeds later has somewhere to be recorded.
func (s *ScriptService) beginRun(ctx context.Context, agentID, trigger, name string) runRecord {
	path, number := s.claimRunLog(ctx, agentID, trigger, name)
	record := runRecord{logPath: path}

	if s.runs == nil || agentID == "" {
		return record
	}

	var logNumber *int32
	if number > 0 {
		claimed := int32(number)
		logNumber = &claimed
	}

	id, err := s.runs.Begin(ctx, runstore.BeginParams{
		AgentID:   agentID,
		Trigger:   trigger,
		Script:    name,
		LogNumber: logNumber,
	})
	if err != nil {
		logging.From(ctx).Warn("failed to record a run of a script", "agent_id", agentID, "script", name, "error", err)

		return record
	}
	record.id = id

	// Bounded by the log-file count so a row never outlives the output it points at.
	if err := s.runs.Prune(ctx, agentID, config.MaxRunLogs); err != nil {
		logging.From(ctx).Warn("failed to prune an agent's runs", "agent_id", agentID, "error", err)
	}

	return record
}

// finishRun closes a run's row with what the script exited on and why it failed, if it did.
func (s *ScriptService) finishRun(ctx context.Context, record runRecord, result scripts.Result, cause error) {
	if s.runs == nil || record.id == "" {
		return
	}

	if err := s.runs.Finish(ctx, runstore.FinishParams{
		ID:       record.id,
		ExitCode: runExitCode(result),
		Failure:  failureReason(cause),
	}); err != nil {
		logging.From(ctx).Warn("failed to close the record of a run", "run_id", record.id, "error", err)
	}
}

// abandonRun drops the row of a run that never started, alongside the empty log removed for the
// same reason: a refusal is not a run and should not count against what an agent keeps.
func (s *ScriptService) abandonRun(ctx context.Context, record runRecord) {
	if s.runs == nil || record.id == "" {
		return
	}

	if err := s.runs.Abandon(ctx, record.id); err != nil {
		logging.From(ctx).Warn("failed to drop the record of a run that never started", "run_id", record.id, "error", err)
	}
}

func runExitCode(result scripts.Result) *int32 {
	if result.ExitCode < 0 {
		return nil
	}

	code := int32(result.ExitCode)

	return &code
}

// pruneRunLogs drops the runs past the last MaxRunLogs, so an agent resumed daily keeps a
// readable handful rather than a file for every day of it.
func pruneRunLogs(ctx context.Context, logsDir, agentID string) {
	runs := config.ListRunLogs(logsDir, agentID)
	if len(runs) <= config.MaxRunLogs {
		return
	}

	for _, run := range runs[config.MaxRunLogs:] {
		if err := os.Remove(run.Path); err != nil && !os.IsNotExist(err) {
			logging.From(ctx).Warn("failed to prune a run log", "path", run.Path, "error", err)
		}
	}
}

func appendNote(ctx context.Context, path string, stderr []byte, note string) []byte {
	line := []byte("[" + note + "]\n")
	if len(stderr) > 0 && stderr[len(stderr)-1] != '\n' {
		line = append([]byte("\n"), line...)
	}

	if path != "" {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			logging.From(ctx).Warn("failed to append to run log", "path", path, "error", err)
		} else {
			if _, err := f.Write(line); err != nil {
				logging.From(ctx).Warn("failed to append to run log", "path", path, "error", err)
			}
			f.Close()
		}
	}

	return tailBytes(append(stderr, line...), setupPaneOutputLimit)
}

// noteEmptyLog puts the reason into a log its run never wrote a byte to, since a pane refused
// before it was respawned leaves the number claimed for it opening an empty pager.
func noteEmptyLog(ctx context.Context, path string, cause error) {
	if path == "" {
		return
	}

	if info, err := os.Stat(path); err != nil || info.Size() > 0 {
		return
	}

	writeRunLog(ctx, path, []byte(cause.Error()+"\n"))
}

func writeRunLog(ctx context.Context, path string, content []byte) {
	if path == "" {
		return
	}

	if err := os.WriteFile(path, content, 0o644); err != nil {
		logging.From(ctx).Warn("failed to write run log", "path", path, "error", err)
	}
}

func readProvisionedDir(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return io.ReadAll(io.LimitReader(file, maxProvisionedDir+1))
}

// RunDeprovision starts the deprovisioner without waiting for it to finish, calling done once
// with nil on success or the failure otherwise, and never when the script refuses to start.
func (s *ScriptService) RunDeprovision(ctx context.Context, agent agentstore.Agent, name string, done func(error)) error {
	script, env, err := s.prepare(ctx, prepareParams{
		agent:   agent,
		name:    name,
		trigger: TriggerDeprovision,
	})
	if err != nil {
		return err
	}

	return s.start(ctx, startParams{
		script:  script,
		env:     env,
		agent:   agent,
		trigger: TriggerDeprovision,
		run:     s.beginRun(ctx, agent.ID, TriggerDeprovision, name),
		done:    done,
	})
}

var ErrScriptAlreadyRunning = errors.New("the script is already running for this agent")

type scriptKey struct {
	agentID string
	name    string
}

// removeEmptyLog drops the file a run claimed but never wrote to, since a refusal is not a run
// and a number spent on one would push a real run out of the fifty that are kept.
func removeEmptyLog(ctx context.Context, path string) {
	if path == "" {
		return
	}

	info, err := os.Stat(path)
	if err != nil || info.Size() > 0 {
		return
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		logging.From(ctx).Warn("failed to remove the log of a run that never started", "path", path, "error", err)
	}
}

func (s *ScriptService) claim(key scriptKey) bool {
	s.inFlightMu.Lock()
	defer s.inFlightMu.Unlock()

	if _, running := s.inFlight[key]; running {
		return false
	}

	s.inFlight[key] = struct{}{}

	return true
}

func (s *ScriptService) release(key scriptKey) {
	s.inFlightMu.Lock()
	defer s.inFlightMu.Unlock()

	delete(s.inFlight, key)
}

type startParams struct {
	script  scripts.Script
	env     []string
	agent   agentstore.Agent
	trigger string
	run     runRecord
	done    func(error)
}

func timeoutFallback(trigger string) time.Duration {
	if trigger == TriggerDeprovision {
		return deprovisionTimeout
	}

	return 0
}

func (s *ScriptService) start(ctx context.Context, p startParams) error {
	key := scriptKey{agentID: p.agent.ID, name: p.script.Name}
	if !s.claim(key) {
		removeEmptyLog(ctx, p.run.logPath)
		s.abandonRun(ctx, p.run)

		return fmt.Errorf("failed to run script %s: %w", p.script.Name, ErrScriptAlreadyRunning)
	}

	logger := s.runLogger(ctx, p.script.Name, p.trigger, p.agent)
	ctx = context.WithoutCancel(ctx)
	began := time.Now()

	err := s.runner.Run(scripts.RunParams{
		Script:  p.script,
		Dir:     p.script.GetDir(),
		Env:     p.env,
		Timeout: p.script.Header.TimeoutOr(timeoutFallback(p.trigger)),
		Done: func(result scripts.Result, err error) {
			defer s.release(key)

			writeRunLog(ctx, p.run.logPath, combinedOutput(result.Stdout, result.Stderr))
			s.finishRun(ctx, p.run, result, err)

			if err != nil {
				logger.LogAttrs(ctx, slog.LevelError, "script failed",
					append(failedScript(result, err), slog.Int64(logging.DurationMS, elapsedMS(began)))...)
			} else {
				logger.LogAttrs(ctx, slog.LevelInfo, "script finished", finishedScript(result, began)...)
			}

			if p.done != nil {
				p.done(err)
			}
		},
	})
	if err != nil {
		s.release(key)
		removeEmptyLog(ctx, p.run.logPath)
		s.abandonRun(ctx, p.run)

		return err
	}

	logger.LogAttrs(ctx, slog.LevelInfo, "script started")

	return nil
}

func (s *ScriptService) runLogger(ctx context.Context, name, trigger string, agent agentstore.Agent) *slog.Logger {
	return logging.From(ctx).With(logging.Attrs([]slog.Attr{
		slog.String(logging.ScriptName, name),
		slog.String(logging.RunTrigger, trigger),
		slog.String(logging.ScriptAgentID, agent.ID),
		slog.String(logging.ScriptAgentCwd, agent.Cwd),
	})...)
}

func elapsedMS(began time.Time) int64 {
	return time.Since(began).Milliseconds()
}

func finishedScript(result scripts.Result, began time.Time) []slog.Attr {
	attrs := []slog.Attr{
		slog.Int(logging.ExitCode, result.ExitCode),
		slog.Int64(logging.DurationMS, elapsedMS(began)),
	}

	return append(attrs, outputAttrs(result)...)
}

func failedScript(result scripts.Result, err error) []slog.Attr {
	attrs := []slog.Attr{slog.Int(logging.ExitCode, result.ExitCode)}
	attrs = append(attrs, logging.Err(err)...)

	return append(attrs, outputAttrs(result)...)
}

func outputAttrs(result scripts.Result) []slog.Attr {
	var attrs []slog.Attr

	if said := bytes.TrimSpace(result.Stdout); len(said) > 0 {
		attrs = append(attrs, slog.String(logging.ScriptStdout, string(said)))
	}
	if said := bytes.TrimSpace(result.Stderr); len(said) > 0 {
		attrs = append(attrs, slog.String(logging.ScriptStderr, string(said)))
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

func scriptDetail(stdout, stderr []byte, logPath string) string {
	stdout = bytes.TrimSpace(stdout)
	stderr = bytes.TrimSpace(stderr)
	total := len(stdout) + len(stderr)
	if total == 0 {
		return ""
	}
	if total <= scriptDetailCap {
		return ": " + string(combinedOutput(stdout, stderr))
	}

	keptStderr := tailBytes(stderr, scriptDetailCap)
	keptStdout := tailBytes(stdout, scriptDetailCap-len(keptStderr))
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
	// hook resolves the name against the hooks directory rather than the built-in scripts directory,
	// since a hook is a script nothing fires by hand: a workflow names it against a moment.
	hook bool
}

func (s *ScriptService) prepare(ctx context.Context, p prepareParams) (scripts.Script, []string, error) {
	find := s.find
	if p.hook {
		find = s.findHook
	}

	script, err := find(p.name)
	if err != nil {
		return scripts.Script{}, nil, err
	}

	env, err := s.env(ctx, scriptEnvParams{
		agent:   p.agent,
		dir:     script.GetDir(),
		trigger: p.trigger,
		script:  script.Name,
	})
	if err != nil {
		return scripts.Script{}, nil, err
	}

	return script, env, nil
}

func (s *ScriptService) find(name string) (scripts.Script, error) {
	return s.catalog.Find(name, s.builtinDirs()...)
}

func (s *ScriptService) findHook(name string) (scripts.Script, error) {
	dirs, err := s.hookDirs()
	if err != nil {
		return scripts.Script{}, err
	}

	return s.catalog.Find(name, dirs...)
}

func (s *ScriptService) hookDirs() ([]string, error) {
	cfg, err := s.load()
	if err != nil {
		return nil, fmt.Errorf("failed to read the config: %w", err)
	}

	dir, err := cfg.GetHooksDir()
	if err != nil {
		return nil, err
	}

	return []string{dir}, nil
}

// builtinDirs is where the scripts weave ships live, which is the only place a script that is
// not a hook comes from now that nothing else can be named in a field.
func (s *ScriptService) builtinDirs() []string {
	return []string{s.builtinDir}
}

type scriptPayload struct {
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

type scriptEnvParams struct {
	agent   agentstore.Agent
	dir     string
	trigger string
	script  string
}

func (s *ScriptService) env(ctx context.Context, p scriptEnvParams) ([]string, error) {
	agent, dir, trigger := p.agent, p.dir, p.trigger

	workflow, err := s.workflows.GetIncludingArchived(ctx, agent.WorkflowID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("failed to read workflow %q: %w", agent.WorkflowID, err)
	}

	payload := scriptPayload{
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
	payload.Prompt, payload.PromptTruncated = head(agent.Prompt, maxScriptPrompt)
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
		"WEAVE_SCRIPT_DIR=" + dir,
		"WEAVE_TRIGGER=" + trigger,
		"WEAVE_SCRIPT=" + p.script,
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

	// An agent that provisions nothing keeps the directory it was given, so its hooks must not
	// be handed WEAVE_WORKTREE_REPO pointing at a worktree nobody made for it.
	if workflow != nil && workflow.Worktree.Enabled && !isProvisionerDisabled(agent.ProvisionEnabled) {
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

func (s *ScriptService) worktreeEnv(workflow workflowstore.Workflow, agent agentstore.Agent) ([]string, error) {
	loaded, err := s.load()
	if err != nil {
		return nil, fmt.Errorf("failed to read the config: %w", err)
	}

	cfg := workflow.Worktree

	// The configured directory is shared by every workflow, so a repository that expects its
	// worktrees in a particular place is served by the workflow naming that place itself.
	worktreesDir := config.ExpandHome(cfg.Dir)
	if worktreesDir == "" {
		worktreesDir, err = loaded.GetWorktreesDir()
		if err != nil {
			return nil, err
		}
	}

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
