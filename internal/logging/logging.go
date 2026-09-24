// Package logging builds the slog logger komrade writes with, carries it on the context so any
// layer can reach it, and turns errors and recovered panics into the attributes a line is
// written with.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"komrade/lib/xslice"
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
	AgentID                = "komrade.agent.id"
	SessionID              = "komrade.session.id"
	TargetAgentID          = "komrade.target_agent.id"
	LifecycleAction        = "komrade.lifecycle.action"
	AgentState             = "komrade.agent.state"
	AgentAlive             = "komrade.agent.alive"
	UnrecognizedFrameCount = "komrade.agent.unrecognized_frame_count"
	MCPTool                = "mcp.tool.name"
	ScriptName             = "komrade.script.name"
	RunTrigger             = "komrade.run.trigger"
	ScriptAgentID          = "komrade.script.agent.id"
	ScriptAgentCwd         = "komrade.script.agent.cwd"
	ScriptStdout           = "komrade.script.stdout"
	ScriptStderr           = "komrade.script.stderr"
	RunID                  = "komrade.run.id"
	ScheduleID             = "komrade.schedule.id"
	FilePath               = "file.path"
	TmuxCommands           = "komrade.tmux.commands"
	MailID                 = "komrade.mail.id"
	UnreadMail             = "komrade.mail.unread"
	ExchangedMail          = "komrade.mail.exchanged"
	WorkflowID             = "komrade.workflow.id"
	ExitCode               = "process.exit.code"
	DurationMS             = "duration_ms"
	Loop                   = "loop"
	RepositoryURL          = "komrade.repository.url"
	ForgeHost              = "komrade.forge.host"
	ForgeCalls             = "komrade.forge.calls"
	RowsInserted           = "komrade.rows.inserted"
	RowsUpdated            = "komrade.rows.updated"
	Observations           = "komrade.pull_request.observations"
	SweepInProgress        = "komrade.pull_request.sweep_in_progress"
	StandingsRead          = "komrade.pull_request.standings_read"
	PullRequestNumber      = "komrade.pull_request.number"
	Repositories           = "komrade.repositories"
	RowsListed             = "komrade.rows.listed"
	SearchCapped           = "komrade.pull_request.search_capped"
	UpdatedThrough         = "komrade.pull_request.updated_through"
	RenamedRepositoryURL   = "komrade.repository.renamed_url"
	ThrottledUntil         = "komrade.forge.throttled_until"
	Repeats                = "repeats"
	WatchTable             = "komrade.watch.table"
	WatchResource          = "komrade.watch.resource"
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

const Service = "komrade"

// ProcessName tells the service's records apart from the panel's, both writing to one file.
const ProcessName = "process.name"

const (
	ProcessServe = "serve"
	ProcessPanel = "panel"
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

	return slog.Default()
}
