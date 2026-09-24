package logging

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"komrade/internal/logtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLogPanic covers the record a panic is written as.
func TestLogPanic(t *testing.T) {
	tests := []struct {
		name        string
		recovered   any
		wantType    string
		wantMessage string
	}{
		{
			name:        "a string is the value it was raised with",
			recovered:   "something gave way",
			wantType:    "string",
			wantMessage: "something gave way",
		},
		{
			name:        "an error reads as its message",
			recovered:   errors.New("the store went away"),
			wantType:    "*errors.errorString",
			wantMessage: "the store went away",
		},
		{
			name:        "and anything else as the value printed",
			recovered:   404,
			wantType:    "int",
			wantMessage: "404",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			ctx := WithLogger(t.Context(), New(&out, slog.LevelError))

			LogPanic(ctx, tt.recovered)

			record := logtest.Record(t, out.String())
			assert.Equal(t, "panicked", record[BodyKey])
			assert.Equal(t, tt.wantType, record[ErrorType])
			assert.Equal(t, tt.wantMessage, record[ErrorMessage])
			assert.Contains(t, record[StackTrace], "panics_test.go",
				"the stack reaches back to the test that raised it")
		})
	}
}

// TestLogAndRepanic covers the guard every root defers, where a call that returns normally leaves
// nothing behind and a panic is written down and raised again.
func TestLogAndRepanic(t *testing.T) {
	tests := []struct {
		name      string
		run       func(ctx context.Context)
		wantPanic bool
	}{
		{
			name: "nothing is written when nothing panicked",
			run: func(ctx context.Context) {
				defer LogAndRepanic(ctx)
			},
		},
		{
			name: "and a panic carries on after it is recorded",
			run: func(ctx context.Context) {
				defer LogAndRepanic(ctx)

				panic("the pane went away")
			},
			wantPanic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			ctx := WithLogger(t.Context(), New(&out, slog.LevelError))

			if !tt.wantPanic {
				tt.run(ctx)
				assert.Empty(t, out.String())

				return
			}

			assert.PanicsWithValue(t, "the pane went away", func() { tt.run(ctx) })

			record := logtest.Record(t, out.String())
			assert.Equal(t, "panicked", record[BodyKey])
			require.Contains(t, record, StackTrace)
			assert.Contains(t, record[StackTrace], "panics_test.go",
				"the stack is where the panic came from rather than the guard's own frames")
		})
	}
}
