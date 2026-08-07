// Package logging is the service's structured log: everything the background service says
// goes through here as one JSON record per line, written and never rendered, so every
// reader works on the same thing. The names follow OpenTelemetry's semantic conventions so
// the log can be shipped to a collector later without every consumer learning new words.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"weave/lib/xslice"
)

// The attribute keys, spelled once here rather than at each call site, since a typo in one
// is invisible in a log.
const (
	// TraceID identifies one request across every line written while serving it; it's
	// otel's trace id in role rather than format, generated per request since nothing here
	// is a span and no caller propagates one.
	TraceID = "trace.id"
	// RPCSystem, RPCService, and RPCMethod name what was called: Connect gives the
	// procedure as one path, split here so the service can be grouped on.
	RPCSystem  = "rpc.system"
	RPCService = "rpc.service"
	RPCMethod  = "rpc.method"
	// RPCErrorCode is the connect code a failed call answered with, which separates a
	// client sending nonsense from the service breaking.
	RPCErrorCode = "rpc.connect_rpc.error_code"
	// ClientAddress is where the call came from.
	ClientAddress = "client.address"
	// HTTPMethod, URLPath, and HTTPStatus describe a call served as plain HTTP rather
	// than through connect, which is how the MCP endpoint is reached.
	HTTPMethod = "http.request.method"
	URLPath    = "url.path"
	HTTPStatus = "http.response.status_code"
	// RequestHost and RequestOrigin are the address a request was aimed at and the page
	// that sent it, carried only on a refused request, where the two of them are the whole
	// of why it was refused.
	RequestHost   = "http.request.header.host"
	RequestOrigin = "http.request.header.origin"
	// AgentID and SessionID name the agent a call came from, set only on calls from an
	// agent the panel spawned; the log is filtered on one to follow an agent and joined on
	// the other to put a call beside what was being said around it.
	AgentID   = "weave.agent.id"
	SessionID = "weave.session.id"
	// TargetAgentID names the agent a call reached for rather than the one that made it.
	TargetAgentID = "weave.target_agent.id"
	// DelegateAgentID names the agent a lifecycle notice is about: the one archived or
	// deleted, rather than TargetAgentID, the parent the notice reached for.
	DelegateAgentID = "weave.delegate_agent.id"
	// LifecycleReason is what happened to a delegate a notice is about, archived or
	// deleted.
	LifecycleReason = "weave.lifecycle.reason"
	// AgentState is the state the reconciler decided an agent is in.
	AgentState = "weave.agent.state"
	// MCPTool is the tool a model asked for; the path is always /mcp, so this is the only
	// field on an MCP call that says what it was trying to do.
	MCPTool = "mcp.tool.name"
	// ActionName and ActionTrigger name the script that ran and what set it off.
	ActionName    = "weave.action.name"
	ActionTrigger = "weave.action.trigger"
	// ActionAgentID and ActionAgentCwd name the agent an action was fired on, which is not
	// AgentID: a model firing an action on another agent is two different agents.
	ActionAgentID  = "weave.action.agent.id"
	ActionAgentCwd = "weave.action.agent.cwd"
	// ActionStderr is the tail of what an action said about itself: carried always for an
	// action waited on, since stderr is where its progress belongs, but only on failure for
	// one nothing waits on.
	ActionStderr = "weave.action.stderr"
	// MailID names one piece of mail between agents, and UnreadMail is how much was
	// waiting when weave last looked.
	MailID     = "weave.mail.id"
	UnreadMail = "weave.mail.unread"
	// WorkflowID names the workflow a line is about.
	WorkflowID = "weave.workflow.id"
	// ExitCode is what a process exited with.
	ExitCode = "process.exit.code"
	// DurationMS is how long the call took, in milliseconds, named rather than left as a
	// duration so it sorts and thresholds as a number.
	DurationMS = "duration_ms"
	// Loop names the background loop a line came from; the service runs three of them
	// against the same store, so a failed pass says nothing without it.
	Loop = "loop"
	// Repeats is how many times a loop has failed the same way, since a pass that keeps
	// failing writes one line and then counts.
	Repeats = "repeats"
	// Handle names a database handle the teardown was closing, since the service holds
	// more than one and a close that timed out says nothing without it.
	Handle = "handle"
	// ServerAddress is where the service is listening.
	ServerAddress = "server.address"
	// ErrorType and ErrorMessage carry a failure; the type is the Go type of the error,
	// which tells two failures apart when both messages are wrapped past recognition.
	ErrorType    = "error.type"
	ErrorMessage = "error.message"
	// StackTrace is where a panic came from, carried only by records standing for one,
	// since an ordinary error already names its origin in its message but a panic value
	// says nothing without the frames behind it.
	StackTrace = "error.stack_trace"
)

// RPCSystemConnect is what every record about a served call carries as its system, the
// value otel reserves for Connect.
const RPCSystemConnect = "connect_rpc"

// The fields every record carries, from the OpenTelemetry log data model rather than
// slog's own; written flat rather than as the nested OTLP envelope since nothing exports
// it yet and flat lines are what greps, leaving the nesting for the exporter's job on the
// day there is one.
const (
	// TimestampKey, SeverityKey, and BodyKey are exported because reading the log back is
	// something weave does itself, in the trace command.
	TimestampKey = "timestamp"
	SeverityKey  = "severity_text"
	BodyKey      = "body"

	severityNum = "severity_number"
)

// ServiceName is the resource every record is attributed to; it's constant, but written
// anyway because a log that doesn't name what produced it can't be merged with another.
const ServiceName = "service.name"

// Service is what this binary calls itself in a record.
const Service = "weave"

// severityNumbers map slog's levels onto the numbers OpenTelemetry orders severity by,
// since the text alone cannot be compared against a threshold.
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

// otelFields renames what slog puts on every record to what the log data model calls it,
// and splits the level into the text and the number that orders it.
func otelFields(groups []string, attr slog.Attr) slog.Attr {
	// Only the record's own fields are renamed; an attribute inside a group was nested
	// deliberately, and a key of "msg" down there belongs to the caller, not slog.
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

		// An empty key inlines the group, so the two fields sit beside each other on the
		// record rather than nested under a level of their own.
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

// loggerKey is what a request's own logger is held under; it's unexported so nothing
// outside this package can collide with it.
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
