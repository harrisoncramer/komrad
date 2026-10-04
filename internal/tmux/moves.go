package tmux

import (
	"log/slog"
	"strconv"
	"strings"

	"komrad/internal/logging"
	"komrad/internal/panes"
)

// zoomedFormat asks whether a pane is the one filling its window, put as a format so tmux can
// answer it inside a conditional.
const zoomedFormat = "#{&&:#{window_zoomed_flag},#{pane_active}}"

// MovePanes runs a change's pane moves in one invocation, which tmux processes in a single pass
// and paints once, so a change moving several panes reaches the terminal as one frame.
func (c *Client) MovePanes(moves []panes.Move) {
	var args []string

	for _, move := range moves {
		for _, command := range argsForMove(move) {
			if len(args) > 0 {
				args = append(args, ";")
			}

			args = append(args, command...)
		}
	}

	if len(args) == 0 {
		return
	}

	if _, err := c.query(args...); err != nil {
		// One invocation reports one status, so the log names every command it held rather than
		// saying only that something in it went wrong.
		attrs := append(logging.Err(err), slog.String(logging.TmuxCommands, strings.Join(args, " ")))
		//nolint:sloglint // the adapter is called for its effect and is handed no context to carry a logger on
		slog.Default().Warn("a tmux pane move failed", logging.Attrs(attrs)...)
	}
}

// argsForMove is the commands one move stands for, two for a respawn.
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
	case panes.KindResize:
		return [][]string{{"resize-pane", "-t", move.Pane, "-x", strconv.Itoa(move.Width)}}
	}

	return nil
}

// zoomArgs is the conditional tmux runs to put a pane's zoom where it is wanted, asked as a
// condition rather than a toggle so it lands whatever an earlier command in the same invocation
// did to the zoom.
func zoomArgs(pane string, zoomed bool) []string {
	resize := "resize-pane -Z -t " + pane

	then, otherwise := "", resize
	if !zoomed {
		then, otherwise = resize, ""
	}

	return []string{"if-shell", "-F", "-t", pane, zoomedFormat, then, otherwise}
}
