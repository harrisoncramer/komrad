// Package tmuxtest runs a real tmux server for a test to drive, on a socket of its own that is
// killed when the test ends.
//
// A fake tmux would only ever agree with what komrad already assumes about tmux, which is
// where the bugs in an adapter live, so these tests spawn panes, type into them, and read
// frames back for real.
package tmuxtest

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"komrad/internal/tmux"
	"komrad/lib/clock"

	"github.com/stretchr/testify/require"
)

const watchdogScript = `while kill -0 "$1" 2>/dev/null; do sleep 1; done; tmux -L "$2" kill-server`

var created atomic.Int64

type Server struct {
	t      *testing.T
	socket string
}

// New starts a tmux server for this test and returns it.
func New(t *testing.T) *Server {
	t.Helper()

	_, err := exec.LookPath("tmux")
	require.NoError(t, err, "tmux is not installed")

	server := &Server{t: t, socket: fmt.Sprintf("komradtest-%d-%d", os.Getpid(), created.Add(1))}

	watchdog := exec.Command("sh", "-c", watchdogScript, "watchdog", strconv.Itoa(os.Getpid()), server.socket)
	watchdog.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	require.NoError(t, watchdog.Start(), "failed to start the tmux server's watchdog")

	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", server.socket, "kill-server").Run()
		_ = watchdog.Process.Kill()
		_ = watchdog.Wait()
	})

	return server
}

// Socket is the name this server is reached by.
func (s *Server) Socket() string {
	return s.socket
}

// Client is a tmux client bound to this server.
func (s *Server) Client() *tmux.Client {
	return tmux.NewClient(s.socket)
}

// Run runs a tmux command against this server and returns its output, failing the test
// when tmux refuses.
func (s *Server) Run(args ...string) string {
	s.t.Helper()

	out, err := exec.Command("tmux", append([]string{"-L", s.socket, "-f", "/dev/null"}, args...)...).Output()
	require.NoError(s.t, err, "tmux %v", args)

	return strings.TrimSpace(string(out))
}

// NewSession starts a detached session.
func (s *Server) NewSession(name string) {
	s.t.Helper()
	s.Run("new-session", "-d", "-s", name, "-c", s.t.TempDir())
}

// Spawn starts a detached session running command and returns its pane id.
func (s *Server) Spawn(name, command string) string {
	s.t.Helper()
	s.Run("new-session", "-d", "-s", name, "-c", s.t.TempDir(), command)

	return s.Run("display-message", "-p", "-t", name, "#{pane_id}")
}

// Kill tears down a session.
func (s *Server) Kill(name string) {
	s.t.Helper()
	s.Run("kill-session", "-t", name)
}

// WaitForFrame polls a pane until its capture contains want, so a test never races the
// shell's first render.
func (s *Server) WaitForFrame(pane, want string) {
	s.t.Helper()

	client := s.Client()
	for range 100 {
		if strings.Contains(client.CapturePane(pane), want) {
			return
		}

		clock.Sleep(s.t.Context(), 50*time.Millisecond)
	}

	s.t.Fatalf("pane %s never showed %q; last frame:\n%s", pane, want, client.CapturePane(pane))
}
