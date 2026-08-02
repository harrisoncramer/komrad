package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"

	"weave/internal/logging"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// The reader for the service's log. It ships with the thing that wrote the file, which is
// what lets it know the field names without being told them, so reading the log needs no
// jq filter and no schema kept in a config somewhere.
//
// It reads the file rather than asking the service, since the log is opened when the
// service is the thing that is broken. That is also why this is a command rather than only
// an RPC: it answers when nothing else does.

// logsCmd prints the service's log, projected to a line per record by default and whole
// when asked. It is what the panel's log key pipes into a pager, and what a person runs in
// a terminal for the same reason.
func logsCmd(state *app) *cobra.Command {
	var output string
	var color string

	cmd := &cobra.Command{
		Use:           "logs",
		Short:         "Print the background service's log",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			colored, err := shouldColorize(color, cmd.OutOrStdout())
			if err != nil {
				return err
			}

			switch output {
			case "short", "json":
			default:
				return fmt.Errorf("unknown output %q, expected short or json", output)
			}

			file, err := os.Open(state.env.LogPath())
			if err != nil {
				return fmt.Errorf("failed to read the service log: %w", err)
			}
			defer func() { _ = file.Close() }()

			return writeLog(cmd.OutOrStdout(), file, output == "short", colored)
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "short", "short or json")
	cmd.Flags().StringVar(&color, "color", "auto", "auto, always, or never")

	return cmd
}

// writeLog renders every record in the log to out. A record longer than bufio's default is
// not something to give up the rest of the file over, so the scanner is given room for the
// longest line a record realistically reaches.
func writeLog(out io.Writer, in io.Reader, short, color bool) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), maxRecordBytes)

	writer := bufio.NewWriter(out)

	for scanner.Scan() {
		line := scanner.Text()
		if short {
			line = logging.Short(line, color)
		}

		if _, err := fmt.Fprintln(writer, line); err != nil {
			return fmt.Errorf("failed to write the service log: %w", err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read the service log: %w", err)
	}

	// Checked rather than deferred, since what is still buffered here is output the caller
	// asked for, and dropping this would end a truncated read on a zero exit code.
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("failed to write the service log: %w", err)
	}

	return nil
}

// maxRecordBytes bounds one record. An error message carrying a query or a stack is the
// long case, and a line past this is not one this can usefully render anyway.
const maxRecordBytes = 1024 * 1024

// shouldColorize decides whether to write escapes. Auto asks whether anything reading this can
// show them, which a pipe cannot, so a log piped into grep is plain and the same command
// in a terminal is not. A pager reading colour wants it asked for outright, since to this
// process a pager is a pipe like any other.
func shouldColorize(mode string, out io.Writer) (bool, error) {
	switch mode {
	case "always":
		return true, nil
	case "never":
		return false, nil
	case "auto":
		file, ok := out.(*os.File)

		return ok && term.IsTerminal(int(file.Fd())), nil
	default:
		return false, fmt.Errorf("unknown color %q, expected auto, always, or never", mode)
	}
}
