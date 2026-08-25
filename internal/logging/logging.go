package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"weave/lib/xslice"
)

const (
	TraceID = "trace.id"

	RPCSystem  = "rpc.system"
	RPCService = "rpc.service"
	RPCMethod  = "rpc.method"

	RPCErrorCode = "rpc.connect_rpc.error_code"

	ClientAddress = "client.address"

	HTTPMethod = "http.request.method"
	URLPath    = "url.path"
	HTTPStatus = "http.response.status_code"

	RequestHost   = "http.request.header.host"
	RequestOrigin = "http.request.header.origin"

	AgentID   = "weave.agent.id"
	SessionID = "weave.session.id"

	TargetAgentID = "weave.target_agent.id"

	DelegateAgentID = "weave.delegate_agent.id"

	LifecycleReason = "weave.lifecycle.reason"

	AgentState = "weave.agent.state"

	MCPTool = "mcp.tool.name"

	ActionName    = "weave.action.name"
	ActionTrigger = "weave.action.trigger"

	ActionAgentID  = "weave.action.agent.id"
	ActionAgentCwd = "weave.action.agent.cwd"

	ActionStdout = "weave.action.stdout"
	ActionStderr = "weave.action.stderr"

	MailID     = "weave.mail.id"
	UnreadMail = "weave.mail.unread"

	WorkflowID = "weave.workflow.id"

	ExitCode = "process.exit.code"

	DurationMS = "duration_ms"

	Loop = "loop"

	Repeats = "repeats"

	Handle = "handle"

	ServerAddress = "server.address"

	ErrorType    = "error.type"
	ErrorMessage = "error.message"

	StackTrace = "error.stack_trace"
)

const RPCSystemConnect = "connect_rpc"

const (
	TimestampKey = "timestamp"
	SeverityKey  = "severity_text"
	BodyKey      = "body"

	severityNum = "severity_number"
)

const ServiceName = "service.name"

const Service = "weave"

var severityNumbers = map[slog.Level]int{
	slog.LevelDebug: 5,
	slog.LevelInfo:  9,
	slog.LevelWarn:  13,
	slog.LevelError: 17,
}

// New builds the service's logger over w at the given level, writing JSON one record per
// line since a key=value line can't be split apart reliably once a wrapped error carries a
// colon and a quote.
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

// ParseLevel turns a configured level into slog's; an unknown word is an error rather than
// a fallback, so a misspelled level is reported instead of leaving the log at a verbosity
// nobody chose.
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

// Err renders an error as its two attributes; every failure logged in the service goes
// through this, so a search for one field finds all of them.
func Err(err error) []slog.Attr {
	if err == nil {
		return nil
	}

	return []slog.Attr{slog.String(ErrorType, fmt.Sprintf("%T", err)), slog.String(ErrorMessage, err.Error())}
}

// Attrs is a list of attributes as With takes them, which is where slog's typed
// attributes and its loose fluent values meet.
func Attrs(attrs []slog.Attr) []any {
	return xslice.Map(attrs, func(attr slog.Attr) any { return attr })
}

type loggerKey struct{}

// WithLogger carries a logger on the context, which is how the attributes an
// interceptor worked out once reach everything the handler under it logs.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// From is the logger carried on the context, or the default one; code below the
// interceptor logs through this, so its lines carry the trace id of the call.
func From(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok && logger != nil {
		return logger
	}

	return slog.Default()
}
