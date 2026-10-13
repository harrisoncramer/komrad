package logview

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"komrad/internal/ui/paint"

	"github.com/charmbracelet/x/ansi"
)

const (
	levelWidth = 5
	bodyWidth  = 30
)

// entry is a decoded server log line, keyed by attribute name.
type entry map[string]any

// field is an attribute the compact view shows and the function that renders it.
type field struct {
	key    string
	render func(palette paint.Palette, record entry, key string) string
}

// visibleFields are the attributes the compact view shows, in the order it shows them.
var visibleFields = []field{
	{"loop", plainField},
	{"http.request.method", plainField},
	{"url.path", plainField},
	{"rpc.method", plainField},
	{"mcp.tool.name", plainField},
	{"komrad.script.name", plainField},
	{"komrad.run.trigger", plainField},
	{"http.response.status_code", statusField},
	{"process.exit.code", exitField},
	{"komrad.mail.unread", unreadField},
	{"duration_ms", durationField},
	{"error.message", errorField},
	{"error", errorField},
	{"err", errorField},
	{"rpc.connect_rpc.error_code", errorCodeField},
}

// Compact is a structured server log line rendered on one row as its time, level, message and
// fields, or the line as it stands when it is not a JSON object.
func Compact(palette paint.Palette, line string) string {
	record := parse(line)
	if record == nil {
		return sanitize(line)
	}

	level, tone := record.level()
	row := append([]string{
		palette.Style(paint.ToneMuted).Render(record.clock()),
		palette.Style(tone).Render(fmt.Sprintf("%-*s", levelWidth, level)),
		record.message(palette, tone),
	}, record.fields(palette)...)

	return strings.TrimRight(strings.Join(row, " "), " ")
}

// parse is the line decoded as a single JSON object, or nil when it is anything else.
func parse(line string) entry {
	decoder := json.NewDecoder(strings.NewReader(line))
	decoder.UseNumber()

	var record entry
	if err := decoder.Decode(&record); err != nil {
		return nil
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil
	}

	return record
}

// clock is the time of day of the line, or blanks when it carries no readable timestamp.
func (e entry) clock() string {
	stamp, err := time.Parse(time.RFC3339Nano, e.text("timestamp"))
	if err != nil {
		return strings.Repeat(" ", len(time.TimeOnly))
	}

	return stamp.Format(time.TimeOnly)
}

// level is the line's severity and the tone it renders in.
func (e entry) level() (string, paint.Tone) {
	severity := strings.ToUpper(e.text("severity_text"))
	switch {
	case strings.HasPrefix(severity, "INFO"):
		return severity, paint.ToneSuccess
	case strings.HasPrefix(severity, "WARN"):
		return severity, paint.ToneWarning
	case strings.HasPrefix(severity, "ERR"), strings.HasPrefix(severity, "FATAL"), strings.HasPrefix(severity, "CRIT"):
		return severity, paint.ToneDanger
	default:
		return severity, paint.ToneMuted
	}
}

// message is the line's body padded to its column, coloured when its level is quiet or an error.
func (e entry) message(palette paint.Palette, tone paint.Tone) string {
	body := e.text("body")
	if tone == paint.ToneMuted || tone == paint.ToneDanger {
		body = palette.Style(tone).Render(body)
	}
	if pad := bodyWidth - ansi.StringWidth(body); pad > 0 {
		body += strings.Repeat(" ", pad)
	}

	return body
}

// fields is each visible field the line carries, rendered in order.
func (e entry) fields(palette paint.Palette) []string {
	var rendered []string
	for _, f := range visibleFields {
		if !e.has(f.key) {
			continue
		}
		if value := f.render(palette, e, f.key); value != "" {
			rendered = append(rendered, value)
		}
	}

	return rendered
}

// text is the value at key on one line, with anything that is not a string written as JSON.
func (e entry) text(key string) string {
	switch value := e[key].(type) {
	case nil:
		return ""
	case string:
		return clean(value)
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return clean(fmt.Sprint(value))
		}

		return clean(string(encoded))
	}
}

// has reports whether the line carries key.
func (e entry) has(key string) bool {
	_, ok := e[key]

	return ok
}

// plainField is the value rendered dimmed as it stands.
func plainField(palette paint.Palette, record entry, key string) string {
	return palette.Style(paint.ToneMuted).Render(record.text(key))
}

// statusField is an HTTP status coloured by its class.
func statusField(palette paint.Palette, record entry, key string) string {
	status := record.text(key)

	return palette.Style(statusTone(status)).Render(status)
}

// exitField is a process exit code, green when it is zero and red otherwise.
func exitField(palette paint.Palette, record entry, key string) string {
	code := record.text(key)
	tone := paint.ToneDanger
	if code == "0" {
		tone = paint.ToneSuccess
	}

	return palette.Style(tone).Render("exit " + code)
}

// unreadField is a count of unread mail.
func unreadField(palette paint.Palette, record entry, key string) string {
	return palette.Style(paint.ToneMuted).Render("unread " + record.text(key))
}

// durationField is a duration in milliseconds.
func durationField(palette paint.Palette, record entry, key string) string {
	return palette.Style(paint.ToneMuted).Render(record.text(key) + "ms")
}

// errorField is an error rendered in red.
func errorField(palette paint.Palette, record entry, key string) string {
	return palette.Style(paint.ToneDanger).Render(record.text(key))
}

// errorCodeField is a connect error code, rendered only when no error message already carries it.
func errorCodeField(palette paint.Palette, record entry, key string) string {
	if record.has("error.message") {
		return ""
	}

	return errorField(palette, record, key)
}

// statusTone is the tone of an HTTP status by its class.
func statusTone(code string) paint.Tone {
	switch {
	case strings.HasPrefix(code, "2"), strings.HasPrefix(code, "3"):
		return paint.ToneSuccess
	case strings.HasPrefix(code, "4"):
		return paint.ToneWarning
	default:
		return paint.ToneDanger
	}
}

// clean is the value with control characters removed and its whitespace collapsed to single spaces.
func clean(value string) string {
	return strings.Join(strings.Fields(paint.StripControls(value)), " ")
}
