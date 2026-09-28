package logread

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"

	"komrad/internal/logging"
)

// RecentBudget is how far back from the end of the log a read of the newest lines looks.
const RecentBudget = 64 << 20

// readChunk is how much of the log one read takes in at a time.
const readChunk = 1 << 20

// agentKeys are the fields a line names an agent in.
var agentKeys = []string{logging.AgentID, logging.TargetAgentID, logging.ScriptAgentID}

// Window is a run of lines about some agents and the offset just past the last line read.
type Window struct {
	Lines  []string
	Offset int64
	Reset  bool
}

// Recent is the newest lines about any of the agents, oldest first.
func (r *Reader) Recent(agentIDs []string, limit int) (Window, error) {
	file, size, err := r.open()
	if err != nil {
		return Window{}, err
	}
	defer func() { _ = file.Close() }()

	return r.recent(file, size, agentIDs, limit)
}

// Since is the lines about any of the agents written after offset, oldest first.
func (r *Reader) Since(agentIDs []string, offset int64, limit int) (Window, error) {
	file, size, err := r.open()
	if err != nil {
		return Window{}, err
	}
	defer func() { _ = file.Close() }()

	if offset > size {
		window, err := r.recent(file, size, agentIDs, limit)
		window.Reset = true

		return window, err
	}

	data := make([]byte, min(int64(r.chunk), size-offset))
	if _, err := file.ReadAt(data, offset); err != nil && !errors.Is(err, io.EOF) {
		return Window{}, fmt.Errorf("failed to read the log at %s: %w", r.path, err)
	}

	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		if len(data) == r.chunk {
			return Window{Offset: offset + int64(len(data))}, nil
		}

		return Window{Offset: offset}, nil
	}

	window := Window{Offset: offset}
	for line := range bytes.SplitAfterSeq(data[:end+1], []byte{'\n'}) {
		window.Offset += int64(len(line))
		if isAbout(line, agentIDs) {
			window.Lines = append(window.Lines, string(bytes.TrimRight(line, "\n")))
		}
		if len(window.Lines) == limit {
			break
		}
	}

	return window, nil
}

func (r *Reader) recent(file *os.File, size int64, agentIDs []string, limit int) (Window, error) {
	var (
		found   []string
		partial []byte
		scanned int
	)

	window := Window{Offset: -1}
	pos := size
	for pos > 0 && len(found) < limit && scanned < r.budget {
		n := min(int64(r.chunk), pos)
		pos -= n
		scanned += int(n)

		data := make([]byte, n, int(n)+len(partial))
		if _, err := file.ReadAt(data, pos); err != nil && !errors.Is(err, io.EOF) {
			return Window{}, fmt.Errorf("failed to read the log at %s: %w", r.path, err)
		}
		data = append(data, partial...)

		if window.Offset < 0 {
			end := bytes.LastIndexByte(data, '\n')
			if end < 0 {
				partial = data
				continue
			}

			window.Offset = pos + int64(end) + 1
			data = data[:end+1]
		}

		lines := bytes.Split(data, []byte{'\n'})
		partial = nil
		if pos > 0 {
			partial = slices.Clone(lines[0])
			lines = lines[1:]
		}

		for i := len(lines) - 1; i >= 0 && len(found) < limit; i-- {
			if isAbout(lines[i], agentIDs) {
				found = append(found, string(lines[i]))
			}
		}
	}

	if window.Offset < 0 {
		window.Offset = 0
		if pos > 0 {
			window.Offset = size
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
