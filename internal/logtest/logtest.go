// Package logtest parses the JSON lines a logger wrote, so a test asserts on the fields of a
// record rather than on the text of it.
package logtest

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Records parses what a logger wrote, one map per line.
func Records(t *testing.T, written string) []map[string]any {
	t.Helper()

	trimmed := strings.TrimSpace(written)
	if trimmed == "" {
		return nil
	}

	lines := strings.Split(trimmed, "\n")
	records := make([]map[string]any, 0, len(lines))

	for _, line := range lines {
		var record map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &record), "every line is one record")
		records = append(records, record)
	}

	return records
}

// Record parses what a logger wrote and insists on exactly one record.
func Record(t *testing.T, written string) map[string]any {
	t.Helper()

	records := Records(t, written)
	require.Len(t, records, 1)

	return records[0]
}

// Bodies are the messages of the records written, in order, for a test that cares how many
// times something was said rather than what came with it.
func Bodies(t *testing.T, written string) []string {
	t.Helper()

	records := Records(t, written)
	bodies := make([]string, 0, len(records))

	for _, record := range records {
		body, _ := record["body"].(string)
		bodies = append(bodies, body)
	}

	return bodies
}
