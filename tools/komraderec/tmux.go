package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	settleInterval = 150 * time.Millisecond
	settleTimeout  = 2 * time.Second
	waitInterval   = 200 * time.Millisecond
	waitTimeout    = 30 * time.Second
	attachTimeout  = 5 * time.Second
)

type tmuxServer struct {
	socket string
	args   []string
}

func (s tmuxServer) run(ctx context.Context, args ...string) (string, error) {
	full := append([]string{"-L", s.socket}, s.args...)
	full = append(full, args...)

	command := exec.CommandContext(ctx, "tmux", full...)
	command.Env = environWithoutTmux()

	out, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tmux -L %s %s: %w: %s", s.socket, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}

	return strings.TrimRight(string(out), "\n"), nil
}

func (s tmuxServer) forget(ctx context.Context, args ...string) {
	_, _ = s.run(ctx, args...) //nolint:errcheck // a client already gone is what this asks for
}

func environWithoutTmux() []string {
	var kept []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "TMUX=") && !strings.HasPrefix(entry, "TMUX_PANE=") {
			kept = append(kept, entry)
		}
	}

	return kept
}

type window struct {
	id      string
	session string
	pane    string
	client  string
}

type instance struct {
	inner     tmuxServer
	outer     tmuxServer
	session   string
	panelPane string
	cols      int
	rows      int
	windows   []window
}

func (i *instance) requirePanel(ctx context.Context) (string, error) {
	if _, err := i.inner.run(ctx, "has-session", "-t", i.session); err != nil {
		return "", fmt.Errorf("%w: no session %q on tmux socket %s, so the instance is not up", err, i.session, i.inner.socket)
	}

	pane, err := i.inner.run(ctx, "show-options", "-gv", "@komrade_panel_pane")
	if err != nil || pane == "" {
		return "", fmt.Errorf("no panel is recorded on tmux socket %s, so the instance is not up", i.inner.socket)
	}

	i.panelPane = pane

	build, err := i.inner.run(ctx, "show-options", "-gv", "@komrade_panel_build")
	if err != nil {
		return "", fmt.Errorf("%w: no build is recorded for the panel on tmux socket %s", err, i.inner.socket)
	}

	return build, nil
}

func (i *instance) syncWindow(ctx context.Context) error {
	panel, err := i.inner.run(ctx, "display-message", "-p", "-t", i.panelPane, "#{window_id}")
	if err != nil {
		return err
	}

	if len(i.windows) > 0 && i.windows[0].id == panel {
		return nil
	}

	for _, old := range i.windows {
		i.detachWindow(ctx, old)
	}

	i.windows = nil

	_, err = i.attachWindow(ctx, panel)

	return err
}

func (i *instance) attachWindow(ctx context.Context, id string) (window, error) {
	session := i.outer.socket + "-" + strings.TrimPrefix(id, "@")
	command := fmt.Sprintf(
		`tmux -L %s -T RGB,256 new-session -t %s -s %s \; set-option status off \; select-window -t %s \; resize-window -t %s -x %d -y %d`,
		i.inner.socket, i.session, session, id, id, i.cols, i.rows)

	pane, err := i.outer.run(ctx,
		"new-session", "-d", "-s", i.outer.socket,
		"-x", fmt.Sprint(i.cols), "-y", fmt.Sprint(i.rows),
		"-P", "-F", "#{pane_id}", command)
	if err != nil {
		return window{}, fmt.Errorf("failed to open a client for window %s: %w", id, err)
	}

	added := window{
		id:      id,
		session: session,
		pane:    pane,
	}

	i.windows = append(i.windows, added)

	client, err := i.awaitClient(ctx, added)
	if err != nil {
		return window{}, err
	}

	added.client = client
	i.windows[len(i.windows)-1] = added

	return added, nil
}

func (i *instance) awaitClient(ctx context.Context, added window) (string, error) {
	deadline := time.Now().Add(attachTimeout)

	for time.Now().Before(deadline) {
		clients, err := i.inner.run(ctx, "list-clients", "-t", added.session, "-F", "#{client_name}")
		if err == nil && clients != "" {
			client, _, _ := strings.Cut(clients, "\n")

			return client, nil
		}

		if err := sleep(ctx, settleInterval); err != nil {
			return "", err
		}
	}

	return "", fmt.Errorf("no client attached to window %s within %s", added.id, attachTimeout)
}

func (i *instance) detachWindow(ctx context.Context, old window) {
	i.inner.forget(ctx, "kill-session", "-t", old.session)
	i.outer.forget(ctx, "kill-pane", "-t", old.pane)
}

func (i *instance) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, old := range i.windows {
		i.inner.forget(ctx, "kill-session", "-t", old.session)
	}

	i.outer.forget(ctx, "kill-server")
	i.windows = nil
}

func (i *instance) sendKeys(ctx context.Context, keys ...string) error {
	if err := i.syncWindow(ctx); err != nil {
		return err
	}

	bound, err := i.rootKeys(ctx)
	if err != nil {
		return err
	}

	current := i.windows[0]

	for _, key := range keys {
		args := []string{"send-keys", "-t", current.session, key}
		if bound[key] {
			args = []string{"send-keys", "-K", "-c", current.client, key}
		}

		if _, err := i.inner.run(ctx, args...); err != nil {
			return err
		}
	}

	return nil
}

func (i *instance) typeText(ctx context.Context, text string) error {
	if err := i.syncWindow(ctx); err != nil {
		return err
	}

	_, err := i.inner.run(ctx, "send-keys", "-t", i.windows[0].session, "-l", text)

	return err
}

func (i *instance) rootKeys(ctx context.Context) (map[string]bool, error) {
	listed, err := i.inner.run(ctx, "list-keys", "-T", "root")
	if err != nil {
		return nil, err
	}

	bound := map[string]bool{}

	for line := range strings.SplitSeq(listed, "\n") {
		fields := strings.Fields(line)
		for n, field := range fields {
			if field == "-T" && n+2 < len(fields) {
				bound[unquoteKey(fields[n+2])] = true

				break
			}
		}
	}

	return bound, nil
}

func unquoteKey(key string) string {
	if len(key) >= 2 && (key[0] == '"' || key[0] == '\'') && key[len(key)-1] == key[0] {
		key = key[1 : len(key)-1]
	}

	return strings.TrimPrefix(key, "\\")
}

func (i *instance) capture(ctx context.Context, withEscapes bool) ([]string, error) {
	if err := i.syncWindow(ctx); err != nil {
		return nil, err
	}

	args := []string{"capture-pane", "-p", "-N", "-t", i.windows[0].pane}
	if withEscapes {
		args = append(args, "-e")
	}

	out, err := i.outer.run(ctx, args...)
	if err != nil {
		return nil, err
	}

	return strings.Split(out, "\n"), nil
}

func (i *instance) settle(ctx context.Context, label string) (frame, error) {
	deadline := time.Now().Add(settleTimeout)
	var last frame

	for attempt := 0; ; attempt++ {
		if err := sleep(ctx, settleInterval); err != nil {
			return frame{}, err
		}

		lines, err := i.capture(ctx, true)
		if err != nil {
			return frame{}, err
		}

		current := stitch(stitchParams{
			lines: lines,
			label: label,
			cols:  i.cols,
			rows:  i.rows,
		})

		if attempt > 0 && current.equals(last) || time.Now().After(deadline) {
			return current, nil
		}

		last = current
	}
}

func (i *instance) await(ctx context.Context, text string) error {
	deadline := time.Now().Add(waitTimeout)

	for time.Now().Before(deadline) {
		lines, err := i.capture(ctx, false)
		if err != nil {
			return err
		}

		if strings.Contains(strings.Join(lines, "\n"), text) {
			return nil
		}

		if err := sleep(ctx, waitInterval); err != nil {
			return err
		}
	}

	return fmt.Errorf("the panel did not show %q within %s", text, waitTimeout)
}

func sleep(ctx context.Context, pause time.Duration) error {
	select {
	case <-ctx.Done():
		return errors.New("interrupted")
	case <-time.After(pause):
		return nil
	}
}
