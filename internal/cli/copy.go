package cli

import (
	"errors"
	"fmt"
	"io"
	"strconv"

	"komrad/internal/tmux"

	"github.com/spf13/cobra"
)

func copyCmd(_ *app) *cobra.Command {
	var socket, copyCommand string

	cmd := &cobra.Command{
		Use:   "copy <file> <start-x> <start-y> <end-x> <end-y> <rectangle>",
		Short: "Copy a selection in visual mode from inside the block that has the keys",
		Long: `Copy a selection in visual mode from inside the block that has the keys.

Visual mode puts a picture of the whole sidebar in tmux's copy-mode, and a copy in
it pipes the selection here with where it runs. This keeps the cells that fall
inside the block the picture was drawn for, held in file, and puts them in tmux's
buffers, on the clipboard, and through the copy-command. Nothing here is meant to
be run by hand.`,
		Args:          cobra.ExactArgs(6),
		Hidden:        true,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := io.Copy(io.Discard, cmd.InOrStdin()); err != nil {
				return fmt.Errorf("failed to read the selection: %w", err)
			}

			if socket == "" {
				return errors.New("--socket names the tmux server to copy into")
			}

			sel, err := parseSelection(args[1:])
			if err != nil {
				return err
			}

			return tmux.NewClientAtPath(socket).CopyFromStandIn(cmd.Context(), args[0], sel, copyCommand)
		},
	}

	cmd.Flags().StringVar(&socket, "socket", "", "path to the tmux server's socket")
	cmd.Flags().StringVar(&copyCommand, "copy-command", "", "command the copy is piped into, as tmux's copy-command")

	return cmd
}

func parseSelection(args []string) (tmux.Selection, error) {
	coords := make([]int, 4)
	for i, arg := range args[:4] {
		n, err := strconv.Atoi(arg)
		if err != nil {
			return tmux.Selection{}, fmt.Errorf("failed to read the selection %q: %w", arg, err)
		}

		coords[i] = n
	}

	return tmux.Selection{
		StartX:    coords[0],
		StartY:    coords[1],
		EndX:      coords[2],
		EndY:      coords[3],
		Rectangle: args[4] == "1",
	}, nil
}
