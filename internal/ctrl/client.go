// Package ctrl is a minimal tmux control-mode (tmux -C) client. It attaches to a
// session, streams a pane's output into a terminal emulator, and renders it —
// giving a live, correctly-sized view of a background agent.
package ctrl

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"

	"github.com/charmbracelet/x/vt"
)

// Client owns a `tmux -C` subprocess attached to one session and emulates one
// pane from its output stream.
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	paneID string

	mu  sync.Mutex
	emu *vt.Emulator
}

// Attach starts a control-mode client for a session, sized to width x height,
// and begins emulating the given pane.
func Attach(socket, session, pane string, width, height int) (*Client, error) {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}

	cmd := exec.Command("tmux", "-L", socket, "-C", "attach-session", "-t", session)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to open control-mode stdin: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to open control-mode stdout: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start control mode: %w", err)
	}

	c := &Client{cmd: cmd, stdin: stdin, paneID: pane, emu: vt.NewEmulator(width, height)}

	// Size the control client so tmux reflows the pane to the preview box and
	// redraws it (which seeds the emulator with the current screen).
	_, _ = fmt.Fprintf(stdin, "refresh-client -C %dx%d\n", width, height)

	go c.read(stdout)

	return c, nil
}

// Render returns the current emulated screen.
func (c *Client) Render() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.emu.Render()
}

// Close tears down the control-mode subprocess.
func (c *Client) Close() {
	if c.stdin != nil {
		_, _ = io.WriteString(c.stdin, "detach-client\n")
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}
}

func (c *Client) read(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "%output ") {
			continue
		}

		rest := line[len("%output "):]
		space := strings.IndexByte(rest, ' ')
		if space < 0 {
			continue
		}

		if rest[:space] != c.paneID {
			continue
		}

		c.mu.Lock()
		_, _ = c.emu.Write(unescape(rest[space+1:]))
		c.mu.Unlock()
	}
}

// unescape decodes tmux control-mode output, where bytes are emitted literally
// except escapes of the form \ooo (octal).
func unescape(s string) []byte {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s)+1 && isOctal(s, i+1) {
			out = append(out, byte((s[i+1]-'0')<<6|(s[i+2]-'0')<<3|(s[i+3]-'0')))
			i += 3
			continue
		}

		out = append(out, s[i])
	}

	return out
}

func isOctal(s string, i int) bool {
	if i+2 >= len(s) {
		return false
	}

	return s[i] >= '0' && s[i] <= '7' && s[i+1] >= '0' && s[i+1] <= '7' && s[i+2] >= '0' && s[i+2] <= '7'
}
