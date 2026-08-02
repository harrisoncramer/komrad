package logging

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestShort covers the line one record is read as. The file holds JSON so a tool can
// consume it, and the projection is what makes it readable: the same record that runs past
// three hundred characters as JSON has to fit a pane beside the panel.
func TestShort(t *testing.T) {
	tests := []struct {
		name   string
		record string
		want   string
	}{
		{
			name: "a served tool call",
			record: `{"timestamp":"2026-07-31T18:45:48.123456-04:00","severity_text":"INFO","severity_number":9,` +
				`"body":"http served","service.name":"weave","trace.id":"e21b9bba-1111-2222-3333-444455556666",` +
				`"http.request.method":"POST","url.path":"/mcp","client.address":"127.0.0.1:1234",` +
				`"weave.agent.id":"019fba5a-7777-8888-9999-aaaabbbbcccc","weave.session.id":"9cb04f0e-dddd",` +
				`"rpc.method":"tools/call","mcp.tool.name":"archive_agent","http.response.status_code":200,"duration_ms":23}`,
			want: "18:45:48 INFO  http served tool=archive_agent method=tools/call agent=019fba5a trace=e21b9bba status=200 took=23ms",
		},
		{
			name: "a refused rpc",
			record: `{"timestamp":"2026-07-31T18:45:48.9-04:00","severity_text":"ERROR","severity_number":17,` +
				`"body":"rpc failed","service.name":"weave","rpc.system":"connect_rpc",` +
				`"rpc.service":"agent.v1.AgentService","rpc.method":"ArchiveAgent","duration_ms":1,` +
				`"rpc.connect_rpc.error_code":"not_found","error.type":"*connect.Error","error.message":"not_found: no such agent"}`,
			want: `18:45:48 ERROR rpc failed method=ArchiveAgent status=not_found took=1ms error="not_found: no such agent"`,
		},
		{
			name: "a loop that keeps failing",
			record: `{"timestamp":"2026-07-31T18:45:48.1-04:00","severity_text":"DEBUG","severity_number":5,` +
				`"body":"loop still failing","service.name":"weave","loop":"reconcile","repeats":8}`,
			want: "18:45:48 DEBUG loop still failing loop=reconcile repeats=8",
		},
		{
			name: "the line the service starts with",
			record: `{"timestamp":"2026-07-31T18:45:48.1-04:00","severity_text":"INFO","severity_number":9,` +
				`"body":"the server is listening","service.name":"weave","server.address":"127.0.0.1:8787"}`,
			want: "18:45:48 INFO  the server is listening addr=127.0.0.1:8787",
		},
		{
			name: "a client probing a route this endpoint does not serve",
			record: `{"timestamp":"2026-07-31T18:45:48.1-04:00","severity_text":"WARN","severity_number":13,` +
				`"body":"http served","service.name":"weave","http.request.method":"GET","url.path":"/mcp",` +
				`"http.response.status_code":405,"duration_ms":0}`,
			want: "18:45:48 WARN  http served method=GET status=405 took=0ms",
		},
		{
			name: "a setup action that refused",
			record: `{"timestamp":"2026-07-31T18:45:48.1-04:00","severity_text":"INFO","severity_number":9,` +
				`"body":"action finished","service.name":"weave","weave.action.name":"wtc",` +
				`"weave.action.trigger":"setup","weave.action.agent.id":"019fc3c2-3dfb-7c31-8e91-c78ab8c2c4ca",` +
				`"weave.action.agent.cwd":"/Users/harrisoncramer/chariot/chariot","process.exit.code":1,` +
				`"weave.action.stderr":"no Branch: line in the prompt","duration_ms":141}`,
			want: `18:45:48 INFO  action finished action=wtc trigger=setup on=019fc3c2 exit=1 took=141ms said="no Branch: line in the prompt"`,
		},
		{
			name: "an action that said more than a line holds keeps what it said last",
			record: `{"timestamp":"2026-07-31T18:45:48.1-04:00","severity_text":"ERROR","severity_number":17,` +
				`"body":"action failed","service.name":"weave","weave.action.name":"wtc",` +
				`"weave.action.trigger":"manual","process.exit.code":2,"weave.action.stderr":"` +
				strings.Repeat("x", 50) + strings.Repeat("y", saidLimit) + `"}`,
			want: "18:45:48 ERROR action failed action=wtc trigger=manual exit=2 said=" +
				cutMark + strings.Repeat("y", saidLimit),
		},
		{
			name: "a model firing an action on another agent",
			record: `{"timestamp":"2026-07-31T18:45:48.1-04:00","severity_text":"INFO","severity_number":9,` +
				`"body":"action started","service.name":"weave","weave.action.name":"open-pr",` +
				`"weave.action.trigger":"manual","weave.agent.id":"019fba5a-7777-8888-9999-aaaabbbbcccc",` +
				`"weave.action.agent.id":"019fc3c2-3dfb-7c31-8e91-c78ab8c2c4ca"}`,
			want: "18:45:48 INFO  action started action=open-pr trigger=manual agent=019fba5a on=019fc3c2",
		},
		{
			name:   "something that is not a record of ours at all",
			record: "panic: runtime error: invalid memory address",
			want:   "panic: runtime error: invalid memory address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Short(tt.record, false))
		})
	}
}

// TestShortFitsAPane covers the point of the projection. A record is written to be
// consumed by a tool and reads at over three hundred characters; the line it becomes has
// to fit beside the panel, where eighty columns is the pane somebody actually has.
func TestShortFitsAPane(t *testing.T) {
	record := `{"timestamp":"2026-07-31T18:45:48.123456-04:00","severity_text":"INFO","severity_number":9,` +
		`"body":"http served","service.name":"weave","trace.id":"e21b9bba-1111-2222-3333-444455556666",` +
		`"http.request.method":"POST","url.path":"/mcp","client.address":"127.0.0.1:1234",` +
		`"weave.agent.id":"019fba5a-7777-8888-9999-aaaabbbbcccc","weave.session.id":"9cb04f0e-dddd-eeee",` +
		`"rpc.method":"tools/call","mcp.tool.name":"archive_agent","http.response.status_code":200,"duration_ms":23}`

	short := Short(record, false)

	assert.Greater(t, len(record), 300, "the record really is the wide thing")
	assert.LessOrEqual(t, len(short), 120)
}

// TestShortColorsTheLevel covers finding a failure by eye in a page of ordinary traffic,
// which is the one thing colour is for here. The escapes wrap the level alone, so a line
// piped somewhere that keeps them is still a line of readable text.
func TestShortColorsTheLevel(t *testing.T) {
	record := `{"timestamp":"2026-07-31T18:45:48.1-04:00","severity_text":"ERROR","body":"rpc failed"}`

	colored := Short(record, true)
	plain := Short(record, false)

	assert.Contains(t, colored, "\033[31mERROR")
	assert.Contains(t, colored, colorReset)
	assert.NotContains(t, plain, "\033[")
	assert.Equal(t, plain, stripEscapes(colored), "colour changes nothing but the escapes")
}

// stripEscapes removes the SGR sequences from a line, so a coloured line can be compared
// against the plain one it should otherwise equal.
func stripEscapes(line string) string {
	var b strings.Builder
	for {
		before, rest, found := strings.Cut(line, "\033[")
		b.WriteString(before)
		if !found {
			return b.String()
		}

		_, line, _ = strings.Cut(rest, "m")
	}
}
