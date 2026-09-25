// Package logread reads the service's own log back, by the trace id its lines carry.
package logread

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"komrad/internal/fail"
	"komrad/internal/logging"
	"komrad/lib/xslice"
)

// ErrNoSuchTrace says nothing in the log was written under the trace asked for.
var ErrNoSuchTrace = fail.NotFound("no line was written under that trace")

// ErrNoLog says the service has written no log yet.
var ErrNoLog = fail.Precondition("the service has no log yet")

// scanBuffer is the longest line the log is read with.
const scanBuffer = 1 << 20

// DefaultRecent is how many traces are listed when the caller names no count.
const DefaultRecent = 20

// Line is one line of the log, with the fields every line carries pulled out of the rest.
type Line struct {
	Time    string
	Level   string
	Message string
	Trace   string
	Fields  map[string]any
}

// Summary is one trace id as the listing shows it.
type Summary struct {
	Trace     string
	StartedAt string
	What      string
	Lines     int
}

// Reader reads one log file.
type Reader struct {
	path string
}

// New builds a reader over the log at path.
func New(path string) *Reader {
	return &Reader{path: path}
}

// Lines is every line written under one trace id, in the order the log holds them.
func (r *Reader) Lines(trace string) ([]Line, error) {
	var lines []Line
	err := r.each(func(line Line) {
		if line.Trace == trace {
			lines = append(lines, line)
		}
	})
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, ErrNoSuchTrace.Saying("no line in %s was written under trace %s", r.path, trace)
	}

	return lines, nil
}

// RecentTraces is the trace ids the log ends with, oldest first.
func (r *Reader) RecentTraces(limit int) ([]Summary, error) {
	if limit <= 0 {
		limit = DefaultRecent
	}

	seen := map[string]*Summary{}
	var order []string
	err := r.each(func(line Line) {
		if line.Trace == "" {
			return
		}

		held, ok := seen[line.Trace]
		if !ok {
			held = &Summary{
				Trace:     line.Trace,
				StartedAt: line.Time,
				What:      Describe(line),
			}
			seen[line.Trace] = held
			order = append(order, line.Trace)
		}

		held.Lines++
		if held.What == "" {
			held.What = Describe(line)
		}
	})
	if err != nil {
		return nil, err
	}

	if len(order) > limit {
		order = order[len(order)-limit:]
	}

	summaries := xslice.Map(order, func(trace string) Summary {
		return *seen[trace]
	})

	return summaries, nil
}

// Describe is what a line says its trace was about: the RPC method, or the loop.
func Describe(line Line) string {
	if method, ok := line.Fields[logging.RPCMethod].(string); ok && method != "" {
		return method
	}

	if loop, ok := line.Fields[logging.Loop].(string); ok && loop != "" {
		return loop + " loop"
	}

	return ""
}

func (r *Reader) each(visit func(Line)) error {
	file, err := os.Open(r.path)
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNoLog.Saying("nothing has been written to %s", r.path)
	}
	if err != nil {
		return fmt.Errorf("failed to read the log at %s: %w", r.path, err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64<<10), scanBuffer)

	for scanner.Scan() {
		var fields map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &fields); err != nil {
			continue
		}

		line := Line{Fields: fields}
		line.Time, _ = fields[logging.TimestampKey].(string)
		line.Level, _ = fields[logging.SeverityKey].(string)
		line.Message, _ = fields[logging.BodyKey].(string)
		line.Trace, _ = fields[logging.TraceID].(string)

		visit(line)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read the log at %s: %w", r.path, err)
	}

	return nil
}
