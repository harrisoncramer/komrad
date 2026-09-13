// Package scripts is the seam in front of the scripts a person writes, which weave runs against
// one agent: the provisioner that makes its directory before it starts, the deprovisioner that
// tears that directory down after it is deleted, and the hooks bound to moments in its life.
//
// A script is an executable file in one of those directories, and its header comments declare
// what it does and how long it may take. weave reads that header for the listing, hands the
// agent to the script as its environment, and holds it to the timeout. Because they are
// ordinary files, what a script does is the person's business rather than weave's, which is
// what makes making a worktree something the tool does not have to know about.
package scripts

import (
	"weave/internal/fail"

	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"weave/internal/logging"
)

var ErrNotExecutable = fail.Precondition("the file is not executable: run chmod +x on it")

var ErrNotStartable = fail.Precondition("the file could not be started")

var ErrDirMissing = fail.Precondition("the directory to run in does not exist")

var ErrFailed = fail.Internal("the script exited non-zero")

const killDelay = 2 * time.Second

const outputLimit = 64 << 10

type Script struct {
	Name       string
	Path       string
	Executable bool
	Header     Header
}

// GetDir is the directory a script runs in.
func (a Script) GetDir() string {
	return filepath.Dir(a.Path)
}

type Runner struct{}

// NewRunner builds the runner.
func NewRunner() Runner {
	return Runner{}
}

// discover lists the scripts in dir by name with what each header declares, treating a missing
// directory as no scripts.
func discover(dir string) ([]Script, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("failed to read the scripts directory %s: %w", dir, err)
	}

	found := make([]Script, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}

		found = append(found, Script{
			Name:       entry.Name(),
			Path:       path,
			Executable: info.Mode().Perm()&0o111 != 0,
			Header:     readHeader(path),
		})
	}

	return found, nil
}

type RunParams struct {
	Script  Script
	Dir     string
	Env     []string
	Timeout time.Duration
	Done    func(Result, error)
}

type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Run starts a script and returns once the process exists rather than once it has finished,
// reporting only a failure to start.
func (r Runner) Run(params RunParams) error {
	if !params.Script.Executable {
		return fmt.Errorf("failed to run script %s: %w", params.Script.Name, ErrNotExecutable)
	}

	if info, err := os.Stat(params.Dir); err != nil || !info.IsDir() {
		return fmt.Errorf("failed to run script %s in %s: %w", params.Script.Name, params.Dir, ErrDirMissing)
	}

	var output tail

	ctx := context.Background()
	cancel := func() {}
	if params.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, params.Timeout)
	}

	// The file is executed directly rather than through a shell, so nothing in the environment
	// is parsed as a command line.
	cmd := exec.CommandContext(ctx, params.Script.Path)
	cmd.Dir = params.Dir
	cmd.Env = append(os.Environ(), params.Env...)
	// Stdin stays nil so a script cannot read keystrokes meant for the panel, and stdout and
	// stderr share one buffer since a script that fails usually does so through a plain echo.
	cmd.Stdout = &output
	cmd.Stderr = &output
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = killGroup(cmd)
	cmd.WaitDelay = killDelay

	if err := cmd.Start(); err != nil {
		cancel()

		return fmt.Errorf("failed to run script %s: %w: %w", params.Script.Name, ErrNotStartable, err)
	}

	go func() {
		defer logging.LogAndRepanic(context.Background())
		defer cancel()

		waitErr := cmd.Wait()
		if expired := ctx.Err(); expired != nil {
			waitErr = fmt.Errorf("%w%s", expired, gaveUpAfter(params.Timeout))
		}

		if params.Done == nil {
			return
		}

		params.Done(r.outcome(outcomeParams{
			script:  params.Script,
			state:   cmd.ProcessState,
			stderr:  output.kept(),
			waitErr: waitErr,
		}))
	}()

	return nil
}

// RunAndWait runs a script and waits for it, returning the result whether or not the script
// succeeded.
func (r Runner) RunAndWait(ctx context.Context, params RunParams) (Result, error) {
	if !params.Script.Executable {
		return Result{ExitCode: noExitCode}, fmt.Errorf("failed to run script %s: %w", params.Script.Name, ErrNotExecutable)
	}

	if info, err := os.Stat(params.Dir); err != nil || !info.IsDir() {
		return Result{ExitCode: noExitCode}, fmt.Errorf("failed to run script %s in %s: %w", params.Script.Name, params.Dir, ErrDirMissing)
	}

	if params.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, params.Timeout)
		defer cancel()
	}

	var stdout, stderr tail

	cmd := exec.CommandContext(ctx, params.Script.Path)
	cmd.Dir = params.Dir
	cmd.Env = append(os.Environ(), params.Env...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Cancelling kills the whole process group, so a script that spawned a build does not leave
	// it running.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = killGroup(cmd)
	cmd.WaitDelay = killDelay

	if err := cmd.Start(); err != nil {
		result := Result{ExitCode: r.exitCode(cmd.ProcessState)}
		if expired := ctx.Err(); expired != nil {
			return result, fmt.Errorf("failed to run script %s: %w%s", params.Script.Name, expired, gaveUpAfter(params.Timeout))
		}

		return result, fmt.Errorf("failed to run script %s: %w: %w", params.Script.Name, ErrNotStartable, err)
	}

	waitErr := cmd.Wait()
	if expired := ctx.Err(); expired != nil {
		result := Result{
			Stdout:   stdout.kept(),
			Stderr:   stderr.kept(),
			ExitCode: r.exitCode(cmd.ProcessState),
		}

		return result, fmt.Errorf("failed to run script %s: %w%s", params.Script.Name, expired, gaveUpAfter(params.Timeout))
	}

	return r.outcome(outcomeParams{
		script:  params.Script,
		state:   cmd.ProcessState,
		stdout:  stdout.kept(),
		stderr:  stderr.kept(),
		waitErr: waitErr,
	})
}

func killGroup(cmd *exec.Cmd) func() error {
	return func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

func gaveUpAfter(timeout time.Duration) string {
	if timeout <= 0 {
		return ""
	}

	return fmt.Sprintf(": gave up after %s", timeout)
}

type outcomeParams struct {
	script  Script
	state   *os.ProcessState
	stdout  []byte
	stderr  []byte
	waitErr error
}

func (r Runner) outcome(params outcomeParams) (Result, error) {
	result := Result{
		Stdout:   params.stdout,
		Stderr:   params.stderr,
		ExitCode: r.exitCode(params.state),
	}
	if result.ExitCode == 0 {
		return result, nil
	}

	return result, fmt.Errorf("failed to run script %s: %w: %w", params.script.Name, ErrFailed, params.waitErr)
}

type tail struct {
	buf bytes.Buffer
}

// Write accepts everything, keeps only what fits, and never reports a short write.
func (t *tail) Write(p []byte) (int, error) {
	t.buf.Write(p)
	if excess := t.buf.Len() - outputLimit; excess > 0 {
		t.buf.Next(excess)
	}

	return len(p), nil
}

func (t *tail) kept() []byte {
	return t.buf.Bytes()
}

// noExitCode is what a process that never ran left behind, which is deliberately not zero.
const noExitCode = -1

func (Runner) exitCode(state *os.ProcessState) int {
	if state == nil {
		return noExitCode
	}

	return state.ExitCode()
}
