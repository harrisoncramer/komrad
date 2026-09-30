package tmux

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"komrad/internal/panes"
	"komrad/lib/clock"
	"komrad/lib/xslice"

	"github.com/charmbracelet/x/ansi"
)

// visualSession is the session on a stand-in's private server that its tiles sit in.
const visualSession = "visual"

// tileIdle is what a tile runs until it is given its slice of the picture.
const tileIdle = "sleep 2147483647"

// standInReady is how long a stand-in waits for its private server to be drawn before giving up.
const standInReady = 2 * time.Second

// standInSpec is what the panel hands a stand-in: where to stand in, the picture, and where the
// borders between its blocks fall.
type standInSpec struct {
	Socket      string   `json:"socket"`
	StandIn     string   `json:"stand_in"`
	Sidebar     string   `json:"sidebar"`
	Window      string   `json:"window"`
	Zoomed      bool     `json:"zoomed"`
	Width       int      `json:"width"`
	Height      int      `json:"height"`
	BorderStyle string   `json:"border_style"`
	Selection   string   `json:"selection"`
	Rows        []string `json:"rows"`
	Divider     int      `json:"divider"`
	Rule        int      `json:"rule"`
	RuleLeft    bool     `json:"rule_left"`
	FocusX      int      `json:"focus_x"`
	FocusY      int      `json:"focus_y"`
	Row         int      `json:"row"`
	Col         int      `json:"col"`
	Keys        []string `json:"keys"`
}

type copyTile struct {
	pane string
	rect panes.Rect
}

// RunCopyStandIn draws the stand-in held in file as tiles on a private tmux server, the focused
// one in copy-mode, swaps a client of it into the sidebar's place, and gives the sidebar back once
// that client goes.
func RunCopyStandIn(ctx context.Context, file string) (err error) {
	spec, err := readStandInSpec(file)
	if err != nil {
		return err
	}

	signal.Notify(make(chan os.Signal, 1), syscall.SIGHUP, syscall.SIGTERM)

	outer := NewClientAtPath(spec.Socket)
	swapped, socket := false, ""
	defer func() {
		restoreSidebar(outer, spec, swapped)
		err = errors.Join(err, removePrivateSocket(ctx, socket))
	}()

	tmuxPath, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("failed to find tmux: %w", err)
	}

	name := "komrad-copy-" + strconv.Itoa(os.Getpid())
	inner := &Client{server: []string{"-L", name, "-f", os.DevNull}}

	tiles := layOutTiles(spec)
	focus := findFocusTile(spec, tiles)

	build, err := buildPrivateServer(spec, tiles, focus, tmuxPath)
	if err != nil {
		return err
	}

	if _, err := inner.query(build...); err != nil {
		inner.run("kill-server")

		return fmt.Errorf("failed to build the stand-in: %w", err)
	}

	socket, err = inner.GetSocketPath()
	if err != nil {
		inner.run("kill-server")

		return fmt.Errorf("failed to find the stand-in's server: %w", err)
	}

	if !waitUntil(ctx, func() bool { return inner.value("display-message", "-p", "-t", focus.pane, "#{pane_in_mode}") == "1" }) {
		inner.run("kill-server")

		return errors.New("the stand-in never entered copy-mode")
	}

	attach := exec.CommandContext(ctx, tmuxPath, "-L", name, "-f", os.DevNull, "attach-session", "-t", visualSession)
	attach.Stdin, attach.Stdout, attach.Stderr = os.Stdin, os.Stdout, os.Stderr
	attach.Env = xslice.Filter(os.Environ(), func(entry string) bool { return !strings.HasPrefix(entry, "TMUX=") })
	if err := attach.Start(); err != nil {
		inner.run("kill-server")

		return fmt.Errorf("failed to attach to the stand-in: %w", err)
	}

	if !waitUntil(ctx, func() bool { return inner.value("list-clients", "-F", "#{client_name}") != "" }) {
		inner.run("kill-server")

		return errors.Join(errors.New("the stand-in never attached"), attach.Wait())
	}

	if _, err := outer.query("swap-pane", "-Z", "-s", spec.StandIn, "-t", spec.Sidebar); err != nil {
		inner.run("kill-server")

		return errors.Join(fmt.Errorf("failed to swap in the stand-in for pane %q: %w", spec.Sidebar, err), attach.Wait())
	}

	swapped = true
	outer.SelectPane(spec.StandIn)

	size := strconv.Itoa(spec.Width) + "x" + strconv.Itoa(spec.Height)
	if !waitUntil(ctx, func() bool { return inner.value("list-clients", "-F", "#{client_width}x#{client_height}") == size }) {
		inner.run("detach-client", "-s", visualSession)
	}
	inner.run("set-hook", "-g", "client-resized", fmt.Sprintf(
		"if-shell -F '#{&&:#{==:#{client_width},%d},#{==:#{client_height},%d}}' '' 'detach-client -s %s'", spec.Width, spec.Height, visualSession,
	))

	if err := attach.Wait(); err != nil {
		return fmt.Errorf("failed to run the stand-in's client: %w", err)
	}

	return nil
}

func readStandInSpec(file string) (*standInSpec, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read the stand-in: %w", err)
	}

	var spec standInSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("failed to read the stand-in: %w", err)
	}

	if err := os.Remove(file); err != nil {
		return nil, fmt.Errorf("failed to remove the stand-in's file: %w", err)
	}

	return &spec, nil
}

func restoreSidebar(outer *Client, spec *standInSpec, swapped bool) {
	if swapped {
		if _, err := outer.query("swap-pane", "-Z", "-s", spec.StandIn, "-t", spec.Sidebar); err != nil {
			outer.run("join-pane", "-h", "-b", "-l", strconv.Itoa(spec.Width), "-s", spec.Sidebar, "-t", spec.Window)
			if spec.Zoomed {
				outer.run("resize-pane", "-Z", "-t", spec.Sidebar)
			}
		}
	}

	outer.SelectPane(spec.Sidebar)
	if len(spec.Keys) > 0 {
		outer.run(append([]string{"send-keys", "-t", spec.Sidebar}, spec.Keys...)...)
	}

	outer.KillPane(spec.StandIn)
}

func layOutTiles(spec *standInSpec) []copyTile {
	tiles := []copyTile{{pane: "%0", rect: panes.Rect{Width: spec.Width, Height: spec.Height}}}

	list := 0
	if spec.Divider > 0 && spec.Divider < spec.Width-1 {
		tiles = []copyTile{
			{pane: "%0", rect: panes.Rect{Width: spec.Divider, Height: spec.Height}},
			{pane: "%1", rect: panes.Rect{Left: spec.Divider + 1, Width: spec.Width - spec.Divider - 1, Height: spec.Height}},
		}
		if !spec.RuleLeft {
			list = 1
		}
	}

	if spec.Rule > 0 && spec.Rule < spec.Height-1 {
		above := tiles[list].rect
		tiles[list].rect.Height = spec.Rule
		tiles = append(tiles, copyTile{
			pane: "%" + strconv.Itoa(len(tiles)),
			rect: panes.Rect{Left: above.Left, Top: spec.Rule + 1, Width: above.Width, Height: spec.Height - spec.Rule - 1},
		})
	}

	return tiles
}

func findFocusTile(spec *standInSpec, tiles []copyTile) copyTile {
	for _, tile := range tiles {
		rect := tile.rect
		if spec.FocusX >= rect.Left && spec.FocusX < rect.Left+rect.Width && spec.FocusY >= rect.Top && spec.FocusY < rect.Top+rect.Height {
			return tile
		}
	}

	return tiles[0]
}

func buildPrivateServer(spec *standInSpec, tiles []copyTile, focus copyTile, tmuxPath string) ([]string, error) {
	commands := [][]string{
		{"new-session", "-d", "-s", visualSession, "-x", strconv.Itoa(spec.Width), "-y", strconv.Itoa(spec.Height), tileIdle},
		{"set-option", "-g", "status", "off"},
		{"set-option", "-g", "prefix", "None"},
		{"set-option", "-g", "prefix2", "None"},
		{"set-option", "-s", "escape-time", "0"},
		{"set-option", "-g", "mouse", "on"},
		{"set-option", "-as", "terminal-features", ",*:RGB,*:clipboard"},
		{"set-option", "-g", "pane-border-status", "off"},
		{"set-option", "-g", "pane-border-lines", "single"},
		{"set-option", "-g", "pane-border-indicators", "off"},
		{"set-option", "-g", "pane-border-style", spec.BorderStyle},
		{"set-option", "-g", "pane-active-border-style", spec.BorderStyle},
		{"unbind-key", "-a", "-T", "root"},
		{"unbind-key", "-a", "-T", "prefix"},
	}
	commands = append(commands, tuneCopyModeCommands(spec.Selection)...)

	detach := "detach-client -s " + visualSession
	commands = append(commands, []string{"set-hook", "-g", "client-detached", "run-shell -b -d 1 -C kill-server"})

	commands = append(commands, xslice.Map(tiles[1:], func(tile copyTile) []string { return splitTile(spec, tiles, tile) })...)

	commands = append(commands, []string{
		"set-hook", "-p", "-t", focus.pane, "pane-mode-changed",
		"if-shell -F -t " + focus.pane + " '#{pane_in_mode}' '' '" + detach + "'",
	})

	var files []string
	for _, tile := range tiles {
		file, err := writeTile(sliceTile(spec.Rows, tile.rect))
		if err != nil {
			return nil, errors.Join(err, removeTiles(files))
		}

		files = append(files, file)

		script := `cat "$0"; rm -f "$0"; exec ` + tileIdle
		if tile.pane == focus.pane {
			script = fmt.Sprintf(`cat "$0"; rm -f "$0"; printf '\033[%d;%dH'; "$1" copy-mode -t "$TMUX_PANE"; exec %s`,
				max(spec.Row-tile.rect.Top+1, 1), max(spec.Col-tile.rect.Left+1, 1), tileIdle)
		}

		commands = append(commands, []string{"respawn-pane", "-k", "-t", tile.pane, "sh", "-c", script, file, tmuxPath})
	}

	commands = append(commands, []string{"select-pane", "-t", focus.pane})

	var args []string
	for i, command := range commands {
		if i > 0 {
			args = append(args, ";")
		}
		args = append(args, command...)
	}

	return args, nil
}

func splitTile(spec *standInSpec, tiles []copyTile, tile copyTile) []string {
	if tile.rect.Top == 0 {
		return []string{"split-window", "-h", "-d", "-t", tiles[0].pane, "-l", strconv.Itoa(tile.rect.Width), tileIdle}
	}

	above := tiles[0].pane
	if !spec.RuleLeft && len(tiles) > 2 {
		above = tiles[1].pane
	}

	return []string{"split-window", "-v", "-d", "-t", above, "-l", strconv.Itoa(tile.rect.Height), tileIdle}
}

func sliceTile(rows []string, rect panes.Rect) string {
	lines := make([]string, 0, rect.Height)
	for y := rect.Top; y < rect.Top+rect.Height; y++ {
		if y >= len(rows) {
			break
		}

		lines = append(lines, ansi.Cut(rows[y], rect.Left, rect.Left+rect.Width)+ansi.ResetStyle)
	}

	return strings.Join(lines, "\n")
}

func writeTile(text string) (string, error) {
	file, err := os.CreateTemp("", "komrad-tile-*")
	if err != nil {
		return "", fmt.Errorf("failed to hold a tile of the stand-in: %w", err)
	}

	_, writeErr := file.WriteString(text)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return "", errors.Join(fmt.Errorf("failed to hold a tile of the stand-in: %w", err), os.Remove(file.Name()))
	}

	return file.Name(), nil
}

func removeTiles(files []string) error {
	return errors.Join(xslice.Map(files, os.Remove)...)
}

func waitUntil(ctx context.Context, done func() bool) bool {
	deadline := time.Now().Add(standInReady)
	for time.Now().Before(deadline) {
		if done() {
			return true
		}

		clock.Sleep(ctx, 10*time.Millisecond)
	}

	return false
}

func removePrivateSocket(ctx context.Context, socket string) error {
	if socket == "" {
		return nil
	}

	isGone := func() bool {
		_, err := NewClientAtPath(socket).query("has-session")

		return err != nil
	}

	if !waitUntil(ctx, isGone) {
		return nil
	}

	if err := os.Remove(socket); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to remove the stand-in's socket: %w", err)
	}

	return nil
}
