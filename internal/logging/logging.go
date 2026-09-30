// Package logging builds the slog logger komrad writes with, carries it on the context so any
// layer can reach it, and turns errors and recovered panics into the attributes a line is
// written with.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"komrad/lib/xslice"
)

const (
	TraceID                = "trace.id"
	RPCSystem              = "rpc.system"
	RPCService             = "rpc.service"
	RPCMethod              = "rpc.method"
	RPCErrorCode           = "rpc.connect_rpc.error_code"
	RPCMessagesSent        = "rpc.stream.messages_sent"
	ClientAddress          = "client.address"
	HTTPMethod             = "http.request.method"
	URLPath                = "url.path"
	HTTPStatus             = "http.response.status_code"
	RequestHost            = "http.request.header.host"
	RequestOrigin          = "http.request.header.origin"
	AgentID                = "komrad.agent.id"
	SessionID              = "komrad.session.id"
	TargetAgentID          = "komrad.target_agent.id"
	LifecycleAction        = "komrad.lifecycle.action"
	AgentState             = "komrad.agent.state"
	AgentAlive             = "komrad.agent.alive"
	UnrecognizedFrameCount = "komrad.agent.unrecognized_frame_count"
	MCPTool                = "mcp.tool.name"
	ScriptName             = "komrad.script.name"
	RunTrigger             = "komrad.run.trigger"
	ScriptAgentID          = "komrad.script.agent.id"
	ScriptAgentCwd         = "komrad.script.agent.cwd"
	ScriptStdout           = "komrad.script.stdout"
	ScriptStderr           = "komrad.script.stderr"
	RunID                  = "komrad.run.id"
	ScheduleID             = "komrad.schedule.id"
	FilePath               = "file.path"
	TmuxCommands           = "komrad.tmux.commands"
	MailID                 = "komrad.mail.id"
	DocumentID             = "komrad.document.id"
	UnreadMail             = "komrad.mail.unread"
	ExchangedMail          = "komrad.mail.exchanged"
	WorkflowID             = "komrad.workflow.id"
	ExitCode               = "process.exit.code"
	DurationMS             = "duration_ms"
	Loop                   = "loop"
	LoopWoken              = "komrad.loop.woken"
	RepositoryURL          = "komrad.repository.url"
	ForgeHost              = "komrad.forge.host"
	ForgeCalls             = "komrad.forge.calls"
	RowsInserted           = "komrad.rows.inserted"
	RowsUpdated            = "komrad.rows.updated"
	Observations           = "komrad.pull_request.observations"
	SweepInProgress        = "komrad.pull_request.sweep_in_progress"
	StandingsRead          = "komrad.pull_request.standings_read"
	ThreadsRead            = "komrad.pull_request.threads_read"
	PullRequestNumber      = "komrad.pull_request.number"
	Repositories           = "komrad.repositories"
	RowsListed             = "komrad.rows.listed"
	SearchCapped           = "komrad.pull_request.search_capped"
	UpdatedThrough         = "komrad.pull_request.updated_through"
	RenamedRepositoryURL   = "komrad.repository.renamed_url"
	ThrottledUntil         = "komrad.forge.throttled_until"
	Repeats                = "repeats"
	WatchTable             = "komrad.watch.table"
	WatchResource          = "komrad.watch.resource"
	Handle                 = "handle"
	ServerAddress          = "server.address"
	ErrorType              = "error.type"
	ErrorMessage           = "error.message"
	StackTrace             = "error.stack_trace"
)

const RPCSystemConnect = "connect_rpc"

const (
	TimestampKey = "timestamp"
	SeverityKey  = "severity_text"
	BodyKey      = "body"
	severityNum  = "severity_number"
)

const ServiceName = "service.name"

const Service = "komrad"

// ProcessName tells the service's records apart from the panel's, both writing to one file.
const ProcessName = "process.name"

const (
	ProcessServe = "serve"
	ProcessPanel = "panel"
	ProcessOpen  = "open"
)

var severityNumbers = map[slog.Level]int{
	slog.LevelDebug: 5,
	slog.LevelInfo:  9,
	slog.LevelWarn:  13,
	slog.LevelError: 17,
}

// New builds the service's logger over w at the given level, writing JSON one record per line.
func New(w io.Writer, level slog.Level) *slog.Logger {
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level, ReplaceAttr: otelFields})

	return slog.New(handler).With(slog.String(ServiceName, Service))
}

func otelFields(groups []string, attr slog.Attr) slog.Attr {
	if len(groups) > 0 {
		return attr
	}

	switch attr.Key {
	case slog.TimeKey:
		return slog.Attr{Key: TimestampKey, Value: attr.Value}
	case slog.MessageKey:
		return slog.Attr{Key: BodyKey, Value: attr.Value}
	case slog.LevelKey:
		level, _ := attr.Value.Any().(slog.Level)

		return slog.Attr{Key: "", Value: slog.GroupValue(
			slog.String(SeverityKey, level.String()),
			slog.Int(severityNum, severityNumbers[level]),
		)}
	default:
		return attr
	}
}

// ParseLevel turns a configured level into slog's, taking an unknown word as an error rather
// than a fallback.
func ParseLevel(level string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug, nil
	case "", "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q", level)
	}
}

// Err renders an error as its two attributes, which every failure logged in the service goes
// through.
func Err(err error) []slog.Attr {
	if err == nil {
		return nil
	}

	return []slog.Attr{slog.String(ErrorType, fmt.Sprintf("%T", err)), slog.String(ErrorMessage, err.Error())}
}

// Attrs is a list of attributes as With takes them.
func Attrs(attrs []slog.Attr) []any {
	return xslice.Map(attrs, func(attr slog.Attr) any { return attr })
}

type loggerKey struct{}

// WithLogger carries a logger on the context, so the attributes an interceptor worked out once
// reach everything the handler under it logs.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// From is the logger carried on the context, or the default one.
func From(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok && logger != nil {
		return logger
	}

	return slog.Default() //nolint:forbidigo // the one fallback every other caller reaches through From
}
