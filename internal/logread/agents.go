package logread

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"

	"komrad/internal/logging"
)

// ScanBudget is how much of the log one read of the lines about some agents looks through.
const ScanBudget = 64 << 20

// agentKeys are the fields a line names an agent in.
var agentKeys = []string{logging.AgentID, logging.TargetAgentID, logging.ScriptAgentID}

// Window is a run of lines about some agents, the offset just past the last line read, and whether more remain past it.
type Window struct {
	Lines  []string
	Offset int64
	Reset  bool
	More   bool
}

// NewestAboutAgents is the last limit lines about any of the agents, oldest first.
func (r *Reader) NewestAboutAgents(ctx context.Context, agentIDs []string, limit int) (Window, error) {
	log, err := r.open()
	if err != nil {
		return Window{}, err
	}
	defer func() { _ = log.file.Close() }()

	return r.newestAboutAgents(ctx, log, agentIDs, limit)
}

// AboutAgentsSince is the lines about any of the agents written after offset, oldest first.
func (r *Reader) AboutAgentsSince(ctx context.Context, agentIDs []string, offset int64, limit int) (Window, error) {
	log, err := r.open()
	if err != nil {
		return Window{}, err
	}
	defer func() { _ = log.file.Close() }()

	// An offset past the end means the log was truncated, so the caller starts over from the newest lines.
	if offset > log.size {
		window, err := r.newestAboutAgents(ctx, log, agentIDs, limit)
		if err != nil {
			return Window{}, err
		}
		window.Reset = true

		return window, nil
	}

	reader, err := r.readFrom(log.file, offset, log.size)
	if err != nil {
		return Window{}, err
	}

	window := Window{Offset: offset}
	for len(window.Lines) < limit {
		if err := ctx.Err(); err != nil {
			return Window{}, fmt.Errorf("stopped reading the log at %s: %w", r.path, err)
		}

		// Past the budget the read stops and says more remains, so one call never reads the whole log.
		if window.Offset-offset >= int64(r.budget) {
			window.More = true

			break
		}

		line, n, err := r.nextLine(reader)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Window{}, err
		}

		window.Offset += n
		if isAbout(line, agentIDs) {
			window.Lines = append(window.Lines, string(bytes.TrimSuffix(line, []byte{'\n'})))
		}
	}

	if len(window.Lines) == limit && window.Offset < log.size {
		window.More = true
	}

	return window, nil
}

// newestAboutAgents reads the last budget of the log forward, keeping the last limit lines about the agents.
func (r *Reader) newestAboutAgents(ctx context.Context, log openLog, agentIDs []string, limit int) (Window, error) {
	start := max(log.size-int64(r.budget), 0)
	window := Window{Offset: start}

	// A read starting partway into the log begins one byte early and drops through the first newline.
	if start > 0 {
		window.Offset = start - 1
	}
	reader, err := r.readFrom(log.file, window.Offset, log.size)
	if err != nil {
		return Window{}, err
	}
	if start > 0 {
		_, n, err := r.nextLine(reader)
		if errors.Is(err, io.EOF) {
			return Window{Offset: start}, nil
		}
		if err != nil {
			return Window{}, err
		}
		window.Offset += n
	}

	for {
		if err := ctx.Err(); err != nil {
			return Window{}, fmt.Errorf("stopped reading the log at %s: %w", r.path, err)
		}

		line, n, err := r.nextLine(reader)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Window{}, err
		}

		window.Offset += n
		if isAbout(line, agentIDs) {
			window.Lines = append(window.Lines, string(bytes.TrimSuffix(line, []byte{'\n'})))
		}
		if len(window.Lines) > limit {
			window.Lines = window.Lines[1:]
		}
	}

	return window, nil
}

// nextLine is the next whole line and the bytes it took, with a line longer than maxLine skipped as empty.
func (r *Reader) nextLine(reader *bufio.Reader) ([]byte, int64, error) {
	var skipped int64
	for {
		// A last line without its newline is still being written, so io.EOF leaves it for the next read.
		line, err := reader.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			skipped += int64(len(line))

			continue
		}
		if errors.Is(err, io.EOF) {
			return nil, 0, io.EOF
		}
		if err != nil {
			return nil, 0, fmt.Errorf("failed to read the log at %s: %w", r.path, err)
		}
		if skipped > 0 {
			return nil, skipped + int64(len(line)), nil
		}

		return line, int64(len(line)), nil
	}
}

func (r *Reader) readFrom(file *os.File, offset, size int64) (*bufio.Reader, error) {
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to read the log at %s: %w", r.path, err)
	}

	return bufio.NewReaderSize(io.LimitReader(file, size-offset), r.maxLine), nil
}

// openLog is the log file and its size when it was opened.
type openLog struct {
	file *os.File
	size int64
}

func (r *Reader) open() (openLog, error) {
	file, err := os.Open(r.path)
	if errors.Is(err, fs.ErrNotExist) {
		return openLog{}, ErrNoLog.Saying("nothing has been written to %s", r.path)
	}
	if err != nil {
		return openLog{}, fmt.Errorf("failed to read the log at %s: %w", r.path, err)
	}

	info, err := file.Stat()
	if err != nil {
		_ = file.Close()

		return openLog{}, fmt.Errorf("failed to read the log at %s: %w", r.path, err)
	}

	return openLog{file: file, size: info.Size()}, nil
}

// isAbout reports whether a line names any of the agents in one of the fields that name an agent.
func isAbout(line []byte, agentIDs []string) bool {
	if !slices.ContainsFunc(agentIDs, func(id string) bool { return bytes.Contains(line, []byte(id)) }) {
		return false
	}

	var fields map[string]any
	if err := json.Unmarshal(line, &fields); err != nil {
		return false
	}

	for _, key := range agentKeys {
		if id, ok := fields[key].(string); ok && slices.Contains(agentIDs, id) {
			return true
		}
	}

	return false
}
