package scriptrun

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"time"

	"weave/internal/logging"
	"weave/internal/scripts"

	agentstore "weave/internal/store/agent"
)

const scriptDetailCap = 400

func (s *ScriptService) runLogger(ctx context.Context, name, trigger string, agent agentstore.Agent) *slog.Logger {
	return logging.From(ctx).With(logging.Attrs([]slog.Attr{
		slog.String(logging.ScriptName, name),
		slog.String(logging.RunTrigger, trigger),
		slog.String(logging.ScriptAgentID, agent.ID),
		slog.String(logging.ScriptAgentCwd, agent.Cwd),
	})...)
}

func elapsedMS(began time.Time) int64 {
	return time.Since(began).Milliseconds()
}

func finishedScript(result scripts.Result, began time.Time) []slog.Attr {
	attrs := []slog.Attr{
		slog.Int(logging.ExitCode, result.ExitCode),
		slog.Int64(logging.DurationMS, elapsedMS(began)),
	}

	return append(attrs, outputAttrs(result)...)
}

func failedScript(result scripts.Result, err error) []slog.Attr {
	attrs := []slog.Attr{slog.Int(logging.ExitCode, result.ExitCode)}
	attrs = append(attrs, logging.Err(err)...)

	return append(attrs, outputAttrs(result)...)
}

func outputAttrs(result scripts.Result) []slog.Attr {
	var attrs []slog.Attr

	if said := bytes.TrimSpace(result.Stdout); len(said) > 0 {
		attrs = append(attrs, slog.String(logging.ScriptStdout, string(said)))
	}
	if said := bytes.TrimSpace(result.Stderr); len(said) > 0 {
		attrs = append(attrs, slog.String(logging.ScriptStderr, string(said)))
	}

	return attrs
}

func combinedOutput(stdout, stderr []byte) []byte {
	if len(stdout) == 0 {
		return stderr
	}
	if len(stderr) == 0 {
		return stdout
	}

	return bytes.Join([][]byte{stdout, stderr}, []byte("\n"))
}

func scriptDetail(stdout, stderr []byte, logPath string) string {
	stdout = bytes.TrimSpace(stdout)
	stderr = bytes.TrimSpace(stderr)
	total := len(stdout) + len(stderr)
	if total == 0 {
		return ""
	}
	if total <= scriptDetailCap {
		return ": " + string(combinedOutput(stdout, stderr))
	}

	keptStderr := tailBytes(stderr, scriptDetailCap)
	keptStdout := tailBytes(stdout, scriptDetailCap-len(keptStderr))
	kept := string(combinedOutput(keptStdout, keptStderr))
	cut := total - len(keptStdout) - len(keptStderr)

	if logPath == "" {
		return fmt.Sprintf(": ...%s (%d earlier bytes cut, the rest was not kept)", kept, cut)
	}

	return fmt.Sprintf(": ...%s (%d earlier bytes cut, kept in %s)", kept, cut, logPath)
}

func tailBytes(b []byte, limit int) []byte {
	if len(b) <= limit {
		return b
	}

	return b[len(b)-limit:]
}
