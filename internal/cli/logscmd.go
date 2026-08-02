package cli

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

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
	var follow bool

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

			return writeLog(cmd.Context(), cmd.OutOrStdout(), file, logOutput{
				short:  output == "short",
				color:  colored,
				follow: follow,
			})
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "short", "short or json")
	cmd.Flags().StringVar(&color, "color", "auto", "auto, always, or never")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep reading, printing records as they are written")

	return cmd
}

// logOutput is how a read of the log is rendered and how it ends.
type logOutput struct {
	// short projects each record onto a line, rather than writing the JSON as it is.
	short bool
	// color writes the escapes the short rendering colours its fields with.
	color bool
	// follow stays at the end of the file, writing records as the service appends them,
	// until the context is cancelled. Without it a read ends at the last record there was
	// when it started.
	follow bool
	// wait is how long a follow sleeps between looks at the end of the file, taking
	// followWait when it is zero. Only a test sets it.
	wait time.Duration
}

// writeLog renders every record in the log to out. A record longer than bufio's default is
// not something to give up the rest of the file over, so the scanner is given room for the
// longest line a record realistically reaches.
func writeLog(ctx context.Context, out io.Writer, in io.Reader, p logOutput) error {
	writer := bufio.NewWriter(out)

	// Wrapped before the scanner rather than checked inside the loop, since to the scanner
	// a follow is a reader whose end has not arrived yet and nothing else.
	if p.follow {
		in = &followReader{ctx: ctx, in: in, wait: cmp.Or(p.wait, followWait), onWait: writer.Flush}
	}

	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), maxRecordBytes)

	for scanner.Scan() {
		line := scanner.Text()
		if p.short {
			line = logging.Short(line, p.color)
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

// followWait is how long a follow sleeps at the end of the file before looking again. It
// is short enough that a record shows up about when the thing that wrote it happened, and
// long enough that a pager left open all day is not a busy loop.
const followWait = 200 * time.Millisecond

// followReader is the log read as a stream that has not ended. The service only ever
// appends to the file and never truncates or rotates it, so the end of it means nothing
// more has been written yet rather than there being nothing more to come, and this waits
// there instead of reporting the end.
//
// Without it a pager on the other end of the pipe has nothing to follow: the command would
// print what the file held and exit, and the pager's own follow would sit on a closed pipe
// no record can ever arrive on.
type followReader struct {
	in     io.Reader
	ctx    context.Context //nolint:containedctx // a reader cannot be handed one per Read, and this ends the wait
	wait   time.Duration
	onWait func() error
}

// Read answers with whatever has been appended, waiting for it when nothing has. A
// cancelled context reads as the end of the file, which ends the scan on the other side
// cleanly rather than as a failure, since a follow is ended rather than broken.
func (r *followReader) Read(p []byte) (int, error) {
	for {
		n, err := r.in.Read(p)
		if n > 0 || (err != nil && !errors.Is(err, io.EOF)) {
			return n, err //nolint:wrapcheck // the reader's own error, passed through as it came
		}

		// Before the sleep rather than after the loop, so what has been rendered reaches
		// the pager now instead of waiting for the buffer to fill or the next record to
		// land.
		if err := r.onWait(); err != nil {
			return 0, err
		}

		select {
		case <-r.ctx.Done():
			return 0, io.EOF
		case <-time.After(r.wait):
		}
	}
}

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
