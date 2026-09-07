package tmux

import (
	"log/slog"
	"strings"

	"weave/internal/logging"
	"weave/internal/panes"
)

// zoomedFormat asks whether a pane is the one filling its window, which is the same question
// IsPaneZoomed asks, put where tmux can answer it for itself.
const zoomedFormat = "#{&&:#{window_zoomed_flag},#{pane_active}}"

// MovePanes runs a change's pane moves in one invocation, which tmux processes in a single pass
// and paints once, so a change that moves several panes reaches the terminal as one frame rather
// than one frame per command.
func (c *Client) MovePanes(moves []panes.Move) {
	if len(moves) == 0 {
		return
	}

	var argv [][]string
	for _, move := range moves {
		argv = append(argv, argsForMove(move)...)
	}

	var args []string
	for i, command := range argv {
		if i > 0 {
			args = append(args, ";")
		}

		args = append(args, command...)
	}

	if _, err := c.query(args...); err != nil {
		attrs := append(logging.Err(err), slog.String("commands", describeCommands(argv)))
		slog.Default().Warn("a tmux pane move failed", logging.Attrs(attrs)...)
	}
}

// argsForMove is the commands one move stands for, which is two for a respawn since the pane's
// scrollback outlives the process that wrote it.
func argsForMove(move panes.Move) [][]string {
	if move.Pane == "" {
		return nil
	}

	switch move.Kind {
	case panes.KindZoom:
		return [][]string{zoomArgs(move.Pane, move.Zoomed)}
	case panes.KindSwap:
		return [][]string{{"swap-pane", "-d", "-s", move.Pane, "-t", move.Other}}
	case panes.KindRespawn:
		return [][]string{
			append([]string{"respawn-pane", "-k", "-t", move.Pane, "-c", move.Dir}, move.Argv...),
			{"clear-history", "-t", move.Pane},
		}
	}

	return nil
}

// zoomArgs is the conditional tmux runs to put a pane's zoom where it is wanted, asked as a
// condition rather than a toggle so that it lands whatever an earlier command in the same
// invocation did to the zoom, and costs nothing when the pane is already there.
func zoomArgs(pane string, zoomed bool) []string {
	resize := "resize-pane -Z -t " + pane

	then, otherwise := "", resize
	if !zoomed {
		then, otherwise = resize, ""
	}

	return []string{"if-shell", "-F", "-t", pane, zoomedFormat, then, otherwise}
}

// describeCommands names what a failed invocation held, since one invocation reports one status
// and the log would otherwise say only that something in it went wrong.
func describeCommands(argv [][]string) string {
	commands := make([]string, 0, len(argv))
	for _, command := range argv {
		commands = append(commands, strings.Join(command, " "))
	}

	return strings.Join(commands, " ; ")
}
