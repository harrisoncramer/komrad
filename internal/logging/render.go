package logging

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Reading the log back. The records are written as JSON because that is what a tool can
// consume, and JSON is a poor thing to read: a record runs past three hundred characters,
// which wraps in any pane narrow enough to sit beside the panel, and a wrapped record is
// four rows of one thing rather than four things.
//
// So the file keeps one shape and the reader projects another, the way journalctl keeps
// records and picks a form with -o. What is dropped here is dropped from the view rather
// than from the file, and the whole record is a flag away.

// column is one field a short line can carry: where to read it from and what to call it.
// The names are short because the record's own are not. OTel spells a field so that it
// means the same thing in somebody else's system, which is right for the file and wrong
// for a pane: "http.response.status_code=200" is thirty characters to say what "status=200"
// says in ten.
type column struct {
	// keys are read in order and the first one present wins, so one column can carry what
	// two kinds of record each call something different.
	keys  []string
	label string
	// short cuts an id to its first characters. An id is here to be recognised and to
	// search the file by, not to be read, and a whole one is half a line.
	short bool
	// suffix is a unit the value reads better with.
	suffix string
}

// columns are what a short line shows, in order, and the projection is deliberate rather
// than "everything left over": a line that carries every field is the JSON record again,
// which is the thing that does not fit. What is dropped is dropped from this view only,
// and -o json is a flag away.
//
// The session id is one of the drops worth naming. It identifies the same call the agent
// id does, and one of them is enough to find the rest.
var columns = []column{
	{keys: []string{MCPTool}, label: "tool"},
	{keys: []string{RPCMethod, HTTPMethod}, label: "method"},
	{keys: []string{Loop}, label: "loop"},
	{keys: []string{AgentID}, label: "agent", short: true},
	{keys: []string{TraceID}, label: "trace", short: true},
	{keys: []string{HTTPStatus, RPCErrorCode}, label: "status"},
	{keys: []string{DurationMS}, label: "took", suffix: "ms"},
	{keys: []string{Repeats}, label: "repeats"},
	{keys: []string{ServerAddress}, label: "addr"},
	{keys: []string{ErrorMessage}, label: "error"},
}

// shortIDLen is how much of an id is enough to tell it from the others in one log.
const shortIDLen = 8

// severityColors are the escapes a level is drawn in, so a failure is findable by eye in a
// page of ordinary traffic. They are the terminal's own sixteen rather than a palette, so
// they suit whatever scheme the reader has.
var severityColors = map[string]string{
	"DEBUG": "\033[90m",
	"INFO":  "\033[32m",
	"WARN":  "\033[33m",
	"ERROR": "\033[31m",
}

const colorReset = "\033[0m"

// Short renders one record as a line: when, how bad, what happened, and the fields behind
// it as pairs. A line that is not a record this wrote is passed through as it is, since a
// log with something else in it is still a log somebody is trying to read.
func Short(line string, color bool) string {
	var record map[string]any
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		return line
	}

	var b strings.Builder
	b.WriteString(clockTime(text(record[timestampKey])))
	b.WriteString(" ")
	b.WriteString(severity(text(record[severityKey]), color))
	b.WriteString(" ")
	b.WriteString(text(record[bodyKey]))

	for _, col := range columns {
		raw, ok := col.read(record)
		if !ok {
			continue
		}

		b.WriteString(" ")
		b.WriteString(col.label)
		b.WriteString("=")
		b.WriteString(value(raw, col))
	}

	return b.String()
}

// read is the first of a column's keys the record carries, or false when it carries none.
// A field present but empty counts as absent: an empty pair says a thing was recorded and
// was nothing, where what happened is that this kind of record has no such field.
func (c column) read(record map[string]any) (any, bool) {
	for _, key := range c.keys {
		raw, ok := record[key]
		if !ok || raw == nil || raw == "" {
			continue
		}

		return raw, true
	}

	return nil, false
}

// clockTime is the time of day out of a timestamp, since a log is read in one sitting and
// the date is the same on every line of it. A timestamp in any other shape is left whole
// rather than cut at a position that means nothing.
func clockTime(stamp string) string {
	_, after, found := strings.Cut(stamp, "T")
	if !found {
		return stamp
	}

	clock, _, _ := strings.Cut(after, ".")

	return clock
}

// severity is the level, padded so the fields after it line up down the page, and coloured
// when the reader can show colour.
func severity(level string, color bool) string {
	padded := fmt.Sprintf("%-5s", level)
	if !color {
		return padded
	}

	escape, ok := severityColors[level]
	if !ok {
		return padded
	}

	return escape + padded + colorReset
}

// value renders one field. A number is printed as one rather than in the exponent form
// JSON decoding leaves it in, an id is cut to what tells it apart from the others, and
// anything with a space in it is quoted so the pairs can still be read as pairs.
func value(raw any, col column) string {
	if number, ok := raw.(float64); ok {
		return strconv.FormatFloat(number, 'f', -1, 64) + col.suffix
	}

	out := text(raw)
	if col.short && len(out) > shortIDLen {
		out = out[:shortIDLen]
	}

	if strings.ContainsAny(out, " \t\"") {
		return strconv.Quote(out)
	}

	return out + col.suffix
}

// text is a decoded JSON value as it should read in a line.
func text(raw any) string {
	switch typed := raw.(type) {
	case nil:
		return ""
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}
