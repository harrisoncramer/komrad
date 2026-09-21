package doctor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"time"

	"golang.org/x/term"
)

// cursorReplyTimeout is how long a terminal has to answer a cursor position query before the
// measurement is given up on, short because a terminal that answers at all answers at once.
const cursorReplyTimeout = 500 * time.Millisecond

// MeasureColumnsOnTTY writes each sample to the controlling terminal and asks where the cursor
// landed, which is the width that terminal drew it in with the font it is using.
func MeasureColumnsOnTTY(ctx context.Context, samples []string) ([]int, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return nil, ErrNoTerminal
	}

	state, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTerminalWouldNotYield, err)
	}
	defer func() { _ = term.Restore(int(os.Stdin.Fd()), state) }() //nolint:errcheck // the terminal is being handed back on the way out of a read-only command

	return measureColumns(ctx, os.Stdin, os.Stdout, samples)
}

// measureColumns is the measurement without the terminal, one sample at a time from the first
// column so a long sample cannot wrap into the answer.
func measureColumns(ctx context.Context, in io.Reader, out io.Writer, samples []string) ([]int, error) {
	columns := make([]int, 0, len(samples))
	reader := newReplyReader(in)
	defer reader.close()

	for _, sample := range samples {
		if _, err := io.WriteString(out, "\r\x1b[K"+sample+"\x1b[6n"); err != nil {
			return nil, fmt.Errorf("failed to write to the terminal: %w", err)
		}

		column, err := reader.readCursorColumn(ctx)
		if err != nil {
			return nil, err
		}

		columns = append(columns, column-1)
	}

	if _, err := io.WriteString(out, "\r\x1b[K"); err != nil {
		return nil, fmt.Errorf("failed to write to the terminal: %w", err)
	}

	return columns, nil
}

// replyReader reads cursor position reports off a terminal.
type replyReader struct {
	chunks    chan []byte
	failed    chan error
	done      chan struct{}
	remaining []byte
}

func newReplyReader(in io.Reader) *replyReader {
	reader := &replyReader{
		chunks: make(chan []byte),
		failed: make(chan error, 1),
		done:   make(chan struct{}),
	}

	go reader.pump(in)

	return reader
}

func (r *replyReader) pump(in io.Reader) {
	for {
		buffer := make([]byte, 32)

		read, err := in.Read(buffer)
		if read > 0 {
			select {
			case r.chunks <- buffer[:read]:
			case <-r.done:
				return
			}
		}
		if err != nil {
			select {
			case r.failed <- err:
			case <-r.done:
			}

			return
		}
	}
}

// close releases the reading goroutine if it is parked on a send, and not if it is parked on a
// read of a terminal nobody is typing at.
func (r *replyReader) close() { close(r.done) }

// readCursorColumn reads one CSI row;column R reply and returns the column, one-based as the
// terminal counts it.
func (r *replyReader) readCursorColumn(ctx context.Context) (int, error) {
	timeout := time.NewTimer(cursorReplyTimeout)
	defer timeout.Stop()

	for {
		column, found, err := parseCursorReply(r.remaining)
		if err != nil {
			return 0, err
		}
		if found {
			r.remaining = nil

			return column, nil
		}

		select {
		case chunk := <-r.chunks:
			r.remaining = append(r.remaining, chunk...)
		case err := <-r.failed:
			return 0, fmt.Errorf("%w: %w", ErrCursorQueryUnanswered, err)
		case <-timeout.C:
			return 0, fmt.Errorf("%w: nothing came back within %s", ErrCursorQueryUnanswered, cursorReplyTimeout)
		case <-ctx.Done():
			return 0, fmt.Errorf("%w: %w", ErrCursorQueryUnanswered, ctx.Err())
		}
	}
}

// parseCursorReply finds a CSI row;column R in what has been read so far, ignoring anything
// before it because a keypress can arrive in the same breath as the answer.
func parseCursorReply(buffer []byte) (column int, found bool, err error) {
	for start := 0; start+1 < len(buffer); start++ {
		if buffer[start] != 0x1b || buffer[start+1] != '[' {
			continue
		}

		end := start + 2
		for end < len(buffer) && buffer[end] != 'R' {
			if buffer[end] != ';' && (buffer[end] < '0' || buffer[end] > '9') {
				break
			}
			end++
		}

		if end >= len(buffer) {
			return 0, false, nil
		}
		if buffer[end] != 'R' {
			continue
		}

		return parseRowColumn(string(buffer[start+2 : end]))
	}

	return 0, false, nil
}

func parseRowColumn(body string) (column int, found bool, err error) {
	for index := range body {
		if body[index] != ';' {
			continue
		}

		parsed, err := strconv.Atoi(body[index+1:])
		if err != nil || parsed < 1 {
			return 0, false, fmt.Errorf("%w: it answered %q", ErrCursorQueryUnanswered, body)
		}

		return parsed, true, nil
	}

	return 0, false, fmt.Errorf("%w: it answered %q", ErrCursorQueryUnanswered, body)
}

// readTerminfo reads one capability out of a named terminfo entry, an absent string capability
// coming back empty rather than as an error.
func readTerminfo(ctx context.Context, term, capability string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "tput", "-T", term, capability).Output()

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	return string(out), nil
}
