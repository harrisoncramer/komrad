package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"weave/internal/logging"

	"github.com/spf13/cobra"
)

// traceScanBuffer is how long a single log line may be before it is skipped. A line carries
// whatever an error said, and a stack trace or a script's captured stderr runs long.
const traceScanBuffer = 1 << 20

// traceCmd follows one thing weave did through the log. Everything the service writes while
// serving a request, or while running one pass of a loop, carries the same trace id, so the
// answer to what happened when I pressed that is one command rather than a read of four
// files.
func traceCmd(state *app) *cobra.Command {
	var recent bool

	cmd := &cobra.Command{
		Use:   "trace [id]",
		Short: "Show every log line written under one trace id",
		Long: "Show every log line weave wrote under one trace id.\n\n" +
			"A trace id covers one request end to end, or one pass of a background loop. The\n" +
			"service returns it on any call that failed, and it is on every line in the log, so\n" +
			"following what happened is a matter of naming it here.\n\n" +
			"Run with --recent instead of an id to list the traces the log ends with, which is\n" +
			"where to start when you know what you did but not what it was called.",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := state.env.LogPath()

			if recent || len(args) == 0 {
				return listRecentTraces(cmd.OutOrStdout(), path)
			}

			return showTrace(cmd.OutOrStdout(), path, args[0])
		},
	}

	cmd.Flags().BoolVar(&recent, "recent", false, "list the traces the log ends with rather than showing one")

	return cmd
}

// logLine is one record from the service's log, in the shape this command reads it.
type logLine struct {
	Time    string
	Level   string
	Message string
	Trace   string
	// Fields is the whole record, since which fields a line carries depends on what wrote it.
	Fields map[string]any
}

// showTrace prints every line written under one id, in the order they were written.
func showTrace(out io.Writer, path, trace string) error {
	var written strings.Builder

	err := eachLogLine(path, func(line logLine) {
		if line.Trace != trace {
			return
		}

		written.WriteString(renderTraceLine(line))
		written.WriteString("\n")
	})
	if err != nil {
		return err
	}

	if written.Len() == 0 {
		return fmt.Errorf("no line in %s was written under trace %s", path, trace)
	}

	return writeOut(out, written.String())
}

// recentTraceLimit is how many traces --recent names. It is the tail of the log rather than
// all of it, since the point is to find what just happened.
const recentTraceLimit = 20

// listRecentTraces names the traces the log ends with, the newest last, with what each one
// was. A caller who knows what they did but not what it was called starts here.
func listRecentTraces(out io.Writer, path string) error {
	type summary struct {
		trace string
		what  string
		when  string
		lines int
	}

	seen := map[string]*summary{}
	order := []string{}

	err := eachLogLine(path, func(line logLine) {
		if line.Trace == "" {
			return
		}

		held, ok := seen[line.Trace]
		if !ok {
			held = &summary{trace: line.Trace, when: line.Time, what: describeTrace(line)}
			seen[line.Trace] = held
			order = append(order, line.Trace)
		}

		held.lines++
		if held.what == "" {
			held.what = describeTrace(line)
		}
	})
	if err != nil {
		return err
	}

	if len(order) > recentTraceLimit {
		order = order[len(order)-recentTraceLimit:]
	}

	if len(order) == 0 {
		return writeOut(out, fmt.Sprintf("nothing in %s carries a trace yet\n", path))
	}

	var written strings.Builder
	for _, trace := range order {
		held := seen[trace]
		written.WriteString(fmt.Sprintf("%s  %-36s  %-28s  %d line(s)\n",
			shortTime(held.when), held.trace, held.what, held.lines))
	}

	return writeOut(out, written.String())
}

// writeOut puts a whole answer on the writer at once. Writing to a pipe somebody closed is
// the one way this fails, which is worth reporting rather than half-printing.
func writeOut(out io.Writer, body string) error {
	if _, err := io.WriteString(out, body); err != nil {
		return fmt.Errorf("failed to write the trace: %w", err)
	}

	return nil
}

// describeTrace names what a trace was: the RPC it served, or the loop whose pass it was.
func describeTrace(line logLine) string {
	if method, ok := line.Fields[logging.RPCMethod].(string); ok && method != "" {
		return method
	}

	if loop, ok := line.Fields[logging.Loop].(string); ok && loop != "" {
		return loop + " loop"
	}

	return ""
}

// renderTraceLine is one line as a person reads it: when, how bad, what happened, and
// whatever else that line carried.
func renderTraceLine(line logLine) string {
	var out strings.Builder

	out.WriteString(shortTime(line.Time))
	out.WriteString("  ")
	out.WriteString(fmt.Sprintf("%-5s", strings.ToLower(line.Level)))
	out.WriteString("  ")
	out.WriteString(line.Message)

	for _, key := range sortedFields(line.Fields) {
		out.WriteString(fmt.Sprintf("  %s=%v", key, line.Fields[key]))
	}

	return out.String()
}

// sortedFields is what a line carried beyond the four every line has, named in a stable
// order so two lines read down the page rather than shuffling.
func sortedFields(fields map[string]any) []string {
	skip := map[string]bool{
		logging.TimestampKey: true, logging.SeverityKey: true, logging.BodyKey: true,
		"severity_number": true, logging.TraceID: true,
		logging.ServiceName: true, logging.RPCSystem: true,
	}

	keys := make([]string, 0, len(fields))
	for key := range fields {
		if !skip[key] {
			keys = append(keys, key)
		}
	}

	sort.Strings(keys)

	return keys
}

// shortTime is the clock part of a timestamp, since every line in one trace happened in the
// same few seconds and the date says nothing.
func shortTime(stamp string) string {
	if len(stamp) < 19 {
		return stamp
	}

	return stamp[11:19]
}

// eachLogLine reads the service's log and hands over every record it can parse. A line that
// will not parse is skipped rather than fatal: the log is appended to by a running process
// and the last line may be half written.
func eachLogLine(path string, visit func(logLine)) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to read the log at %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64<<10), traceScanBuffer)

	for scanner.Scan() {
		var fields map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &fields); err != nil {
			continue
		}

		line := logLine{Fields: fields}
		line.Time, _ = fields[logging.TimestampKey].(string)
		line.Level, _ = fields[logging.SeverityKey].(string)
		line.Message, _ = fields[logging.BodyKey].(string)
		line.Trace, _ = fields[logging.TraceID].(string)

		visit(line)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read the log at %s: %w", path, err)
	}

	return nil
}
