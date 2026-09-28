package logread

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"

	"komrad/internal/logging"

	"github.com/icza/backscanner"
)

// ScanBudget is how much of the log one read of the lines about some agents looks through.
const ScanBudget = 64 << 20

// readChunk is how much of the log is read from disk at a time.
const readChunk = 64 << 10

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
func (r *Reader) NewestAboutAgents(agentIDs []string, limit int) (Window, error) {
	file, size, err := r.open()
	if err != nil {
		return Window{}, err
	}
	defer func() { _ = file.Close() }()

	return r.newestAboutAgents(file, size, agentIDs, limit)
}

// AboutAgentsSince is the lines about any of the agents written after offset, oldest first.
func (r *Reader) AboutAgentsSince(agentIDs []string, offset int64, limit int) (Window, error) {
	file, size, err := r.open()
	if err != nil {
		return Window{}, err
	}
	defer func() { _ = file.Close() }()

	// An offset past the end means the log was truncated, so the caller starts over from the newest lines.
	if offset > size {
		window, err := r.newestAboutAgents(file, size, agentIDs, limit)
		window.Reset = true

		return window, err
	}

	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return Window{}, fmt.Errorf("failed to read the log at %s: %w", r.path, err)
	}
	reader := bufio.NewReaderSize(io.LimitReader(file, size-offset), r.chunk)

	window := Window{Offset: offset}
	for len(window.Lines) < limit {
		// Past the budget the read stops and says more remains, so one call never reads the whole log.
		if window.Offset-offset >= int64(r.budget) {
			window.More = true

			break
		}

		// A last line without its newline is still being written, so it is left for the next read.
		line, err := reader.ReadBytes('\n')
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Window{}, fmt.Errorf("failed to read the log at %s: %w", r.path, err)
		}

		window.Offset += int64(len(line))
		if isAbout(line, agentIDs) {
			window.Lines = append(window.Lines, string(bytes.TrimSuffix(line, []byte{'\n'})))
		}
	}

	if len(window.Lines) == limit && window.Offset < size {
		window.More = true
	}

	return window, nil
}

// newestAboutAgents reads the log backward from its end, stopping at limit lines or the budget.
func (r *Reader) newestAboutAgents(file *os.File, size int64, agentIDs []string, limit int) (Window, error) {
	scanner := backscanner.NewOptions(file, int(size), &backscanner.Options{ChunkSize: r.chunk, MaxBufferSize: scanBuffer})

	var found []string
	window := Window{Offset: size}
	for len(found) < limit {
		line, pos, err := scanner.LineBytes()
		if errors.Is(err, io.EOF) || errors.Is(err, backscanner.ErrLongLine) {
			break
		}
		if err != nil {
			return Window{}, fmt.Errorf("failed to read the log at %s: %w", r.path, err)
		}

		// Only the last line can run to the end of the file, and then it has no newline yet, so the next read starts at it.
		if int64(pos+len(line)) == size {
			window.Offset = int64(pos)

			continue
		}

		if size-int64(pos) > int64(r.budget) {
			break
		}

		if isAbout(line, agentIDs) {
			found = append(found, string(line))
		}
	}

	slices.Reverse(found)
	window.Lines = found

	return window, nil
}

func (r *Reader) open() (*os.File, int64, error) {
	file, err := os.Open(r.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, ErrNoLog.Saying("nothing has been written to %s", r.path)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read the log at %s: %w", r.path, err)
	}

	info, err := file.Stat()
	if err != nil {
		_ = file.Close()

		return nil, 0, fmt.Errorf("failed to read the log at %s: %w", r.path, err)
	}

	return file, info.Size(), nil
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
