// Package pty runs a program on a pseudo-terminal.
package pty

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"syscall"

	creackpty "github.com/creack/pty"
)

// defaultTerm is the terminal type a program is told it draws on when the client names none.
const defaultTerm = "xterm-256color"

// Size is a terminal's size in character cells.
type Size struct {
	Rows uint16
	Cols uint16
}

// Terminal is a program running on a pseudo-terminal: what it draws is read, what is typed is
// written, and closing it hangs up.
type Terminal interface {
	io.ReadWriteCloser
	Resize(size Size) error
	Wait() (int, error)
}

// Session is a program started on a pseudo-terminal of its own.
type Session struct {
	command *exec.Cmd
	file    *os.File
}

// StartParams is what a program is started with.
type StartParams struct {
	Argv []string
	Dir  string
	// Env is the program's environment, this process's own when nil.
	Env  []string
	Size Size
	Term string
}

// Start runs a program on a new pseudo-terminal, killed when ctx ends.
func Start(ctx context.Context, params StartParams) (*Session, error) {
	if len(params.Argv) == 0 {
		return nil, errors.New("no program to start")
	}

	term := params.Term
	if term == "" {
		term = defaultTerm
	}

	command := exec.CommandContext(ctx, params.Argv[0], params.Argv[1:]...) //nolint:gosec // the engine names the program
	command.Dir = params.Dir
	env := params.Env
	if env == nil {
		env = os.Environ()
	}
	command.Env = append(slices.Clip(env), "TERM="+term)

	file, err := creackpty.StartWithSize(command, &creackpty.Winsize{Rows: params.Size.Rows, Cols: params.Size.Cols})
	if err != nil {
		return nil, fmt.Errorf("failed to start %s on a terminal: %w", params.Argv[0], err)
	}

	polled, err := pollFile(file)
	if err != nil {
		killErr := command.Process.Kill()
		_, waitErr := command.Process.Wait()

		return nil, errors.Join(err, killErr, waitErr)
	}

	return &Session{command: command, file: polled}, nil
}

// Read reads what the program drew.
func (s *Session) Read(p []byte) (int, error) {
	return s.file.Read(p)
}

// Write types into the program.
func (s *Session) Write(p []byte) (int, error) {
	return s.file.Write(p)
}

// Resize tells the program its terminal changed size.
func (s *Session) Resize(size Size) error {
	if err := creackpty.Setsize(s.file, &creackpty.Winsize{Rows: size.Rows, Cols: size.Cols}); err != nil {
		return fmt.Errorf("failed to resize the terminal: %w", err)
	}

	return nil
}

// Wait waits for the program to end and returns its exit status, 128 plus the signal for a
// program a signal ended.
func (s *Session) Wait() (int, error) {
	err := s.command.Wait()
	if err == nil {
		return 0, nil
	}

	var exited *exec.ExitError
	if !errors.As(err, &exited) {
		return 0, fmt.Errorf("failed to wait for the program: %w", err)
	}

	status, ok := exited.Sys().(syscall.WaitStatus)
	if ok && status.Signaled() {
		return 128 + int(status.Signal()), nil
	}

	return exited.ExitCode(), nil
}

// Close hangs up the terminal.
func (s *Session) Close() error {
	return s.file.Close()
}

// pollFile hands the terminal to the runtime poller.
func pollFile(file *os.File) (*os.File, error) {
	fd, err := syscall.Dup(int(file.Fd())) //nolint:gosec // a descriptor fits an int
	if err != nil {
		_ = file.Close()

		return nil, fmt.Errorf("failed to take over the terminal: %w", err)
	}

	_ = file.Close()

	if err := syscall.SetNonblock(fd, true); err != nil {
		return nil, errors.Join(fmt.Errorf("failed to take over the terminal: %w", err), syscall.Close(fd))
	}

	return os.NewFile(uintptr(fd), "/dev/ptmx"), nil
}
