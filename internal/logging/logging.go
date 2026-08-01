// Package logging is the service's structured log. Everything the background service has
// to say goes through here in one shape, so a line reads by eye in the pager the panel
// opens and greps by field.
//
// The names are OpenTelemetry's semantic conventions. Nothing exports to a collector yet,
// but borrowing the vocabulary means the log can be shipped later without every consumer
// learning new words for the same things.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// The attribute keys, spelled once here rather than at each call site, since a typo in one
// is invisible in a log.
const (
	// TraceID identifies one request across every line written while serving it. It is
	// otel's trace id in role rather than format, generated per request since nothing here
	// is a span and no caller propagates one.
	TraceID = "trace.id"
	// RPCSystem, RPCService, and RPCMethod name what was called. Connect gives the
	// procedure as one path, which is split so the service can be grouped on.
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
	// that sent it. They are carried only on a refused request, where the two of them are
	// the whole of why it was refused.
	RequestHost   = "http.request.header.host"
	RequestOrigin = "http.request.header.origin"
	// AgentID and SessionID name the agent a call came from, set only on calls from an
	// agent the panel spawned. They are what the log is filtered by to follow one agent,
	// and what a call is joined on to put it beside what was being said around it.
	AgentID   = "weave.agent.id"
	SessionID = "weave.session.id"
	// TargetAgentID names the agent a call reached for rather than the one that made it,
	// so a record carrying both is one agent acting on another.
	TargetAgentID = "weave.target_agent.id"
	// MCPTool is the tool a model asked for. The path is /mcp whatever it wanted, so this
	// is the only field on an MCP call that says what it was trying to do.
	MCPTool = "mcp.tool.name"
	// DurationMS is how long the call took, in milliseconds, named rather than left as a
	// duration so it sorts and thresholds as a number.
	DurationMS = "duration_ms"
	// Loop names the background loop a line came from. The service runs three of them
	// against the same store, so a failed pass says nothing without it.
	Loop = "loop"
	// Repeats is how many times a loop has failed the same way, since a pass that keeps
	// failing writes one line and then counts.
	Repeats = "repeats"
	// ServerAddress is where the service is listening.
	ServerAddress = "server.address"
	// ErrorType and ErrorMessage carry a failure. The type is the Go type of the error,
	// which tells two failures apart when both messages are wrapped past recognition.
	ErrorType    = "error.type"
	ErrorMessage = "error.message"
	// StackTrace is where a panic came from, carried only by the records standing for one.
	// An ordinary error names where it came from in its message, where a panic value says
	// nothing at all without the frames behind it.
	StackTrace = "error.stack_trace"
)

// RPCSystemConnect is what every record about a served call carries as its system, the
// value otel reserves for Connect.
const RPCSystemConnect = "connect_rpc"

// The fields every record carries, which are the OpenTelemetry log data model's rather
// than slog's own. A record is written flat rather than as the nested OTLP envelope, since
// nothing exports it yet and flat lines are what greps. The nesting is the exporter's job
// on the day there is one.
const (
	timestampKey = "timestamp"
	severityKey  = "severity_text"
	severityNum  = "severity_number"
	bodyKey      = "body"
)

// ServiceName is the resource every record is attributed to. It is constant, and written
// anyway because a log that does not name what produced it cannot be merged with another.
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

// New builds the service's logger over w at the given level. It writes JSON, one record
// per line, since a key=value line cannot be split apart reliably once a wrapped error
// carries a colon and a quote.
func New(w io.Writer, level slog.Level) *slog.Logger {
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level, ReplaceAttr: otelFields})

	return slog.New(handler).With(slog.String(ServiceName, Service))
}

// otelFields renames what slog puts on every record to what the log data model calls it,
// and splits the level into the text and the number that orders it.
func otelFields(groups []string, attr slog.Attr) slog.Attr {
	// Only the record's own fields are renamed. An attribute inside a group was nested
	// deliberately, and a key of "msg" down there is the caller's rather than slog's.
	if len(groups) > 0 {
		return attr
	}

	switch attr.Key {
	case slog.TimeKey:
		return slog.Attr{Key: timestampKey, Value: attr.Value}
	case slog.MessageKey:
		return slog.Attr{Key: bodyKey, Value: attr.Value}
	case slog.LevelKey:
		level, _ := attr.Value.Any().(slog.Level)

		// An empty key inlines the group, so the two fields sit beside each other on the
		// record rather than nested under a level of their own.
		return slog.Attr{Key: "", Value: slog.GroupValue(
			slog.String(severityKey, level.String()),
			slog.Int(severityNum, severityNumbers[level]),
		)}
	default:
		return attr
	}
}

// ParseLevel turns a configured level into slog's. An unknown word is an error rather than
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

// Err renders an error as its two attributes. Every failure logged in the service goes
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
	loose := make([]any, 0, len(attrs))
	for _, attr := range attrs {
		loose = append(loose, attr)
	}

	return loose
}

// loggerKey is what a request's own logger is held under. It is an unexported type so
// nothing outside this package can collide with it.
type loggerKey struct{}

// WithLogger carries a logger on the context, which is how the attributes an
// interceptor worked out once reach everything the handler under it logs.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// From is the logger carried on the context, or the default one. Code below the
// interceptor logs through this, so its lines carry the trace id of the call.
func From(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok && logger != nil {
		return logger
	}

	return slog.Default()
}
