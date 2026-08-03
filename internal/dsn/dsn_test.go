package dsn

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRedact covers the one thing this is for: a password never coming out. The config
// service hands the whole config to MCP callers, so anything this lets through reaches a
// model reading a tool result.
func TestRedact(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "empty stays empty rather than becoming a complaint",
			in:   "",
			want: "",
		},
		{
			name: "a string that will not parse is not passed through",
			in:   "not a dsn at all",
			want: "(unparseable)",
		},
		{
			name: "the password is dropped and the login and address kept",
			in:   "postgres://weave:secret@db.internal:5432/panel?sslmode=require",
			want: "postgres://weave@db.internal:5432/panel",
		},
		{
			name: "a keyword-list dsn is redacted too, since pgconn parses those as well",
			in:   "host=db.internal port=5432 dbname=panel user=weave password=secret",
			want: "postgres://weave@db.internal:5432/panel",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Redact(tt.in)

			assert.Equal(t, tt.want, got)
			if tt.in != "" {
				assert.NotContains(t, got, "secret", "the password never survives")
			}
		})
	}
}
