package logview

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"komrad/internal/ui/paint"

	"github.com/charmbracelet/x/ansi"
)

const bodyWidth = 30

var hiddenFields = map[string]bool{
	"timestamp":                  true,
	"severity_text":              true,
	"severity_number":            true,
	"body":                       true,
	"service.name":               true,
	"process.name":               true,
	"trace.id":                   true,
	"client.address":             true,
	"rpc.system":                 true,
	"rpc.service":                true,
	"rpc.connect_rpc.error_code": true,
	"error.type":                 true,
	"komrad.agent.id":            true,
	"komrad.session.id":          true,
	"komrad.target_agent.id":     true,
	"komrad.script.agent.id":     true,
	"komrad.script.agent.cwd":    true,
	"komrad.script.stdout":       true,
	"komrad.script.stderr":       true,
	"komrad.mail.id":             true,
}

type entry map[string]any

// Compact is a structured service log line drawn on one row as its time, level, message and
// fields, or the line as it stands when it is not a JSON object.
func Compact(palette paint.Palette, line string) string {
	decoder := json.NewDecoder(strings.NewReader(line))
	decoder.UseNumber()

	var e entry
	if err := decoder.Decode(&e); err != nil || e == nil || decoder.Decode(&struct{}{}) != io.EOF {
		return sanitize(line)
	}

	muted := palette.Style(paint.ToneMuted)
	label, tone := e.level()

	body := e.text("body")
	if tone == paint.ToneMuted || tone == paint.ToneDanger {
		body = palette.Style(tone).Render(body)
	}
	if pad := bodyWidth - ansi.StringWidth(body); pad > 0 {
		body += strings.Repeat(" ", pad)
	}

	parts := []string{muted.Render(e.clock()), palette.Style(tone).Render(label), body}

	if fields := e.fields(palette); len(fields) > 0 {
		parts = append(parts, strings.Join(fields, " "))
	}

	return strings.TrimRight(strings.Join(parts, " "), " ")
}

func (e entry) text(key string) string {
	switch value := e[key].(type) {
	case nil:
		return ""
	case string:
		return clean(value)
	case map[string]any, []any:
		encoded, err := json.Marshal(value)
		if err != nil {
			return clean(fmt.Sprint(value))
		}

		return clean(string(encoded))
	default:
		return clean(fmt.Sprint(value))
	}
}

func (e entry) has(key string) bool {
	_, ok := e[key]

	return ok
}

func (e entry) clock() string {
	stamp, err := time.Parse(time.RFC3339Nano, e.text("timestamp"))
	if err != nil {
		return strings.Repeat(" ", len(time.TimeOnly))
	}

	return stamp.Format(time.TimeOnly)
}

func (e entry) level() (string, paint.Tone) {
	severity := strings.ToUpper(e.text("severity_text"))
	switch {
	case strings.HasPrefix(severity, "DEBUG"):
		return "DBG", paint.ToneMuted
	case strings.HasPrefix(severity, "INFO"):
		return "INF", paint.ToneSuccess
	case strings.HasPrefix(severity, "WARN"):
		return "WRN", paint.ToneWarning
	case strings.HasPrefix(severity, "ERR"), strings.HasPrefix(severity, "FATAL"), strings.HasPrefix(severity, "CRIT"):
		return "ERR", paint.ToneDanger
	case severity == "":
		return "???", paint.ToneMuted
	default:
		return fmt.Sprintf("%-3.3s", severity), paint.ToneMuted
	}
}

func (e entry) fields(palette paint.Palette) []string {
	muted := palette.Style(paint.ToneMuted)
	danger := palette.Style(paint.ToneDanger)
	shown := make(map[string]bool)

	var fields []string
	add := func(rendered string, keys ...string) {
		for _, key := range keys {
			shown[key] = true
		}
		if rendered != "" {
			fields = append(fields, rendered)
		}
	}
	plain := func(key string) {
		if e.has(key) {
			add(muted.Render(e.text(key)), key)
		}
	}

	plain("loop")
	if e.has("http.request.method") || e.has("url.path") {
		add(muted.Render(strings.TrimSpace(e.text("http.request.method")+" "+e.text("url.path"))), "http.request.method", "url.path")
	}
	plain("rpc.method")
	plain("mcp.tool.name")
	plain("komrad.script.name")
	plain("komrad.run.trigger")
	if e.has("http.response.status_code") {
		add(palette.Style(statusTone(e.text("http.response.status_code"))).Render(e.text("http.response.status_code")), "http.response.status_code")
	}
	if e.has("process.exit.code") {
		tone := paint.ToneDanger
		if e.text("process.exit.code") == "0" {
			tone = paint.ToneSuccess
		}
		add(palette.Style(tone).Render("exit "+e.text("process.exit.code")), "process.exit.code")
	}
	if e.has("komrad.mail.unread") {
		add(muted.Render("unread "+e.text("komrad.mail.unread")), "komrad.mail.unread")
	}
	if e.has("duration_ms") {
		add(muted.Render(e.text("duration_ms")+"ms"), "duration_ms")
	}

	for _, key := range slices.Sorted(maps.Keys(e)) {
		if hiddenFields[key] || shown[key] || isError(key) {
			continue
		}
		add(muted.Render(clean(key)+"="+e.text(key)), key)
	}

	for _, key := range []string{"error.message", "error", "err"} {
		if e.has(key) {
			add(danger.Render(e.text(key)), key)
		}
	}
	if !e.has("error.message") && e.has("rpc.connect_rpc.error_code") {
		add(danger.Render(e.text("rpc.connect_rpc.error_code")))
	}

	return fields
}

func isError(key string) bool {
	return key == "error.message" || key == "error" || key == "err"
}

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

func clean(value string) string {
	return strings.Join(strings.Fields(paint.StripControls(value)), " ")
}
