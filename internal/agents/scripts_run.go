package agents

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"time"

	"weave/internal/logging"
	"weave/internal/scripts"

	agentstore "weave/internal/store/agent"
)

const provisionTimeout = 10 * time.Minute

const deprovisionTimeout = 2 * time.Minute

const hookTimeout = 10 * time.Minute

const maxProvisionedDir = 4096

const setupDirFileVar = "WEAVE_SETUP_DIR_FILE"

var ErrProvisionFailed = errors.New("the provisioner failed")

var ErrHookFailed = errors.New("a hook failed")

var ErrScriptAlreadyRunning = errors.New("the script is already running for this agent")

type scriptRunner interface {
	Run(p scripts.RunParams) error
	RunAndWait(ctx context.Context, p scripts.RunParams) (scripts.Result, error)
}

type scriptKey struct {
	agentID string
	name    string
}

type prepareParams struct {
	agent   agentstore.Agent
	name    string
	trigger string
	// hook resolves the name against the hooks directory rather than the built-in scripts
	// directory.
	hook bool
}

// RunProvision runs the provisioner that owns an agent's directory before creating it.
func (s *ScriptService) RunProvision(ctx context.Context, agent agentstore.Agent, name string) (string, error) {
	return s.runAwaited(ctx, prepareParams{
		agent:   agent,
		name:    name,
		trigger: TriggerProvision,
	})
}

// RunHook runs one of a workflow's hooks and waits for it, in the directory the agent was
// provisioned rather than the one the hook was found in.
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
		s.failRun(ctx, record, result, failed)

		return failed
	}

	s.finishRun(ctx, record, result, nil)
	logger.LogAttrs(ctx, slog.LevelInfo, "script finished", finishedScript(result, began)...)

	return nil
}

// refuseHook puts the reason a hook never started into the log the panel offers for it.
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
	})
}

func (s *ScriptService) runAwaited(ctx context.Context, p prepareParams) (string, error) {
	script, env, err := s.prepare(ctx, p)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrProvisionFailed, err)
	}

	logger := s.runLogger(ctx, p.name, p.trigger, p.agent)

	dirFile, err := os.CreateTemp("", "weave-provision-dir-*")
	if err != nil {
		return "", fmt.Errorf("%w: failed to prepare a place for %s to name its directory: %w", ErrProvisionFailed, p.name, err)
	}
	dirPath := dirFile.Name()
	closeErr := dirFile.Close()
	defer func() {
		if removeErr := os.Remove(dirPath); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			logger.LogAttrs(ctx, slog.LevelWarn,
				"failed to remove the provisioner's directory file", logging.Err(removeErr)...)
		}
	}()
	if closeErr != nil {
		return "", fmt.Errorf("%w: failed to prepare a place for %s to name its directory: %w", ErrProvisionFailed, p.name, closeErr)
	}

	env = append(env, setupDirFileVar+"="+dirPath)

	logger.LogAttrs(ctx, slog.LevelInfo, "script started")
	began := time.Now()

	record := s.beginRun(ctx, p.agent.ID, p.trigger, p.name)

	result, err := s.runner.RunAndWait(ctx, scripts.RunParams{
		Script:  script,
		Dir:     script.GetDir(),
		Env:     env,
		Timeout: script.Header.TimeoutOr(provisionTimeout),
	})
	writeRunLog(ctx, record.logPath, combinedOutput(result.Stdout, result.Stderr))

	logger.LogAttrs(ctx, slog.LevelInfo, "script finished", finishedScript(result, began)...)

	if err != nil {
		failed := fmt.Errorf("%w: %w%s", ErrProvisionFailed, err, scriptDetail(result.Stdout, result.Stderr, record.logPath))
		s.failRun(ctx, record, result, failed)

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

	run := s.beginRun(ctx, agent.ID, TriggerDeprovision, name)

	key := scriptKey{agentID: agent.ID, name: script.Name}
	if !s.claim(key) {
		removeEmptyLog(ctx, run.logPath)
		s.abandonRun(ctx, run)

		return fmt.Errorf("failed to run script %s: %w", script.Name, ErrScriptAlreadyRunning)
	}

	logger := s.runLogger(ctx, script.Name, TriggerDeprovision, agent)
	ctx = context.WithoutCancel(ctx)
	began := time.Now()

	err = s.runner.Run(scripts.RunParams{
		Script:  script,
		Dir:     script.GetDir(),
		Env:     env,
		Timeout: script.Header.TimeoutOr(deprovisionTimeout),
		Done: func(result scripts.Result, err error) {
			defer s.release(key)

			writeRunLog(ctx, run.logPath, combinedOutput(result.Stdout, result.Stderr))
			if err != nil {
				s.failRun(ctx, run, result, fmt.Errorf("%s: %w", script.Name, err))
				logger.LogAttrs(ctx, slog.LevelError, "script failed",
					append(failedScript(result, err), slog.Int64(logging.DurationMS, elapsedMS(began)))...)
			} else {
				s.finishRun(ctx, run, result, nil)
				logger.LogAttrs(ctx, slog.LevelInfo, "script finished", finishedScript(result, began)...)
			}

			if done != nil {
				done(err)
			}
		},
	})
	if err != nil {
		s.release(key)
		removeEmptyLog(ctx, run.logPath)
		s.abandonRun(ctx, run)

		return err
	}

	logger.LogAttrs(ctx, slog.LevelInfo, "script started")

	return nil
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

func readProvisionedDir(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return io.ReadAll(io.LimitReader(file, maxProvisionedDir+1))
}
