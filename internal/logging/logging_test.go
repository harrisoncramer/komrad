package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseLevel covers the levels a config can carry: an unknown word is an error rather
// than a fallback, since the fallback slog itself would take is debug, which turns the log
// up instead of saying the word was wrong.
func TestParseLevel(t *testing.T) {
	tests := []struct {
		name    string
		level   string
		want    slog.Level
		wantErr bool
	}{
		{name: "unset", want: slog.LevelInfo},
		{
			name:  "debug",
			level: "debug",
			want:  slog.LevelDebug,
		},
		{
			name:  "in capitals",
			level: "WARN",
			want:  slog.LevelWarn,
		},
		{
			name:  "padded, as a config file leaves it",
			level: " error ",
			want:  slog.LevelError,
		},
		{
			name:    "not a level",
			level:   "verbose",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level, err := ParseLevel(tt.level)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, level)
		})
	}
}

// TestErrIsTwoFields covers how a failure is written: the type is carried alongside the
// message because a message wrapped through three layers is no longer recognisable, and
// the type is what still tells two failures apart.
func TestErrIsTwoFields(t *testing.T) {
	var out bytes.Buffer
	wrapped := fmt.Errorf("failed to list agents: %w", errors.New("connection refused"))

	New(&out, slog.LevelInfo).LogAttrs(t.Context(), slog.LevelError, "loop pass failed", Err(wrapped)...)

	var record map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &record))
	assert.Equal(t, "*fmt.wrapError", record[ErrorType])
	assert.Equal(t, "failed to list agents: connection refused", record[ErrorMessage])
}

// TestErrOfNothing covers a nil error, which adds no fields rather than a line claiming a
// failure with nothing in it.
func TestErrOfNothing(t *testing.T) {
	assert.Empty(t, Err(nil))
}

// TestContextCarriesTheRequestsLogger covers what an interceptor leaves behind, which is
// how a handler's own lines come out under the same fields as the record for the call that
// reached it.
func TestContextCarriesTheRequestsLogger(t *testing.T) {
	var out bytes.Buffer
	logger := New(&out, slog.LevelInfo).With(slog.String(TraceID, "abc-123"))

	ctx := WithLogger(t.Context(), logger)
	From(ctx).InfoContext(ctx, "the handler said something")

	var record map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &record))
	assert.Equal(t, "abc-123", record[TraceID])
	assert.Equal(t, Service, record[ServiceName], "every record names what produced it")
}

// TestContextWithoutALogger covers code reached from somewhere other than a request, which
// logs to the default rather than to nothing at all.
func TestContextWithoutALogger(t *testing.T) {
	assert.Equal(t, slog.Default(), From(t.Context()))
}
