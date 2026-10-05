package logview_test

import (
	"testing"

	"komrad/internal/ui/logview"
	"komrad/internal/ui/paint"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

// TestCompact covers the text of a service log line drawn on one row.
func TestCompact(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
		want string
	}{
		{
			name: "an http request shows its route, status and duration",
			line: `{"timestamp":"2026-10-04T21:37:02.11-04:00","severity_text":"WARN","severity_number":13,"body":"http served","service.name":"komrad","process.name":"komrad","trace.id":"t","http.request.method":"GET","url.path":"/mcp","client.address":"127.0.0.1","http.response.status_code":405,"duration_ms":1}`,
			want: "21:37:02 WARN  http served                    GET /mcp 405 1ms",
		},
		{
			name: "a failed rpc shows its method and error on the same row",
			line: `{"timestamp":"2026-10-04T21:38:25.151009-04:00","severity_text":"ERROR","body":"rpc failed","rpc.system":"connect_rpc","rpc.service":"agent.v1.AgentService","rpc.method":"CreateReviewThread","duration_ms":1,"rpc.connect_rpc.error_code":"invalid_argument","error.type":"*connect.Error","error.message":"invalid_argument: validation error:\n - body: too long"}`,
			want: "21:38:25 ERROR rpc failed                     CreateReviewThread 1ms invalid_argument: validation error: - body: too long",
		},
		{
			name: "an error code with no message stands in for it",
			line: `{"severity_text":"ERROR","body":"rpc failed","rpc.connect_rpc.error_code":"not_found"}`,
			want: "         ERROR rpc failed                     not_found",
		},
		{
			name: "a finished script shows its name, trigger, exit code and duration but not its output",
			line: `{"timestamp":"2026-10-04T21:36:58-04:00","severity_text":"INFO","body":"script finished","komrad.script.name":"_worktree_setup","komrad.run.trigger":"provision","komrad.script.agent.cwd":"/tmp/x","process.exit.code":0,"duration_ms":811,"komrad.script.stderr":"fetching\ncreating"}`,
			want: "21:36:58 INFO  script finished                _worktree_setup provision exit 0 811ms",
		},
		{
			name: "a mail nudge shows its loop and unread count",
			line: `{"severity_text":"INFO","body":"told an agent to read its mail","loop":"delivery","trace.id":"t","komrad.target_agent.id":"a","komrad.mail.unread":1}`,
			want: "         INFO  told an agent to read its mail delivery unread 1",
		},
		{
			name: "fields the compact view does not show are left out",
			line: `{"severity_text":"INFO","body":"producer: Producer job counts","queue":"default","num_jobs_running":0,"num_completed_jobs":12}`,
			want: "         INFO  producer: Producer job counts",
		},
		{
			name: "a nested value is shown as json",
			line: `{"severity_text":"INFO","body":"b","error":{"a":1.50,"b":["x y","z"]}}`,
			want: `         INFO  b                              {"a":1.50,"b":["x y","z"]}`,
		},
		{
			name: "a debug line with no fields keeps no trailing space",
			line: `{"severity_text":"DEBUG","body":"frame read"}`,
			want: "         DEBUG frame read",
		},
		{
			name: "a timestamp the parser rejects keeps its column blank",
			line: `{"timestamp":"2026-10-04 21:37:02","severity_text":"INFO","body":"b"}`,
			want: "         INFO  b",
		},
		{
			name: "a line with no severity keeps its column blank",
			line: `{"body":"hello"}`,
			want: "               hello",
		},
		{
			name: "escape sequences inside values are stripped",
			line: `{"severity_text":"INFO","body":"\u001b[31mred\u001b[0m","loop":"a\tb"}`,
			want: "         INFO  red                            a b",
		},
		{
			name: "escape sequences inside a status code and a severity are stripped",
			line: `{"severity_text":"x\nyz","body":"b","http.response.status_code":"405\n\u001b[2J","a\nb":"c"}`,
			want: "         XYZ   b                              405",
		},
		{
			name: "a severity past error is drawn as it stands",
			line: `{"severity_text":"CRITICAL","body":"b"}`,
			want: "         CRITICAL b",
		},
		{
			name: "a line that is not json is drawn as it stands",
			line: "plain \x1b[1mscript\x1b[0m output",
			want: "plain script output",
		},
		{
			name: "an object followed by more data is drawn as it stands",
			line: `{"body":"a"}{"body":"b"}`,
			want: `{"body":"a"}{"body":"b"}`,
		},
		{
			name: "an object followed by text is drawn as it stands",
			line: `{"body":"a"} trailing`,
			want: `{"body":"a"} trailing`,
		},
		{
			name: "a json value that is not an object is drawn as it stands",
			line: `[1,2]`,
			want: "[1,2]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, ansi.Strip(logview.Compact(paint.Panel, tt.line)))
		})
	}
}

// TestCompactTones covers the colour each level and status takes.
func TestCompactTones(t *testing.T) {
	t.Parallel()

	palette := paint.Panel
	line := logview.Compact(palette, `{"severity_text":"ERROR","body":"rpc failed","http.response.status_code":500,"error.message":"boom"}`)

	assert.Contains(t, line, palette.Style(paint.ToneDanger).Render("ERROR"))
	assert.Contains(t, line, palette.Style(paint.ToneDanger).Render("500"))
	assert.Contains(t, line, palette.Style(paint.ToneDanger).Render("boom"))
}
