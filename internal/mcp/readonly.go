package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const (
	// maxRows bounds how many rows a single query returns. The query is wrapped
	// with LIMIT maxRows+1 so we can tell the caller when results were truncated.
	maxRows = 1000
	// maxCellChars truncates any single stringified cell so one wide column
	// cannot blow up the response. Transcripts are stored whole, so this matters.
	maxCellChars = 2000
	// queryTimeout caps how long a query may run. It is set both as the context
	// deadline and as the session's statement_timeout, so a query that stops
	// responding is cut off by the server rather than left running behind a
	// caller who has already given up.
	queryTimeout = 15 * time.Second
)

// forbiddenPattern matches any write, DDL, or connection-state verb. A statement
// containing one is rejected before it ever reaches the database. It runs over
// the statement's executable text only, with literals, quoted identifiers, and
// comments blanked out first, so searching transcripts for the word "update" is
// a search and not a rejected write.
var forbiddenPattern = regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE|DROP|ALTER|TRUNCATE|CREATE|GRANT|REVOKE|EXECUTE|CALL|MERGE|VACUUM|REINDEX|ATTACH|DETACH|PRAGMA|BEGIN|COMMIT|ROLLBACK|SET)\b`)

// replaceStatement matches REPLACE only where it begins a statement, which is
// the write form (REPLACE INTO). SQLite also has a replace() scalar function,
// and rejecting every use of the word turned an ordinary string substitution
// into a refused write.
var replaceStatement = regexp.MustCompile(`(?i)(\A|[(;])\s*REPLACE\s+INTO\b`)

// queryResult is the JSON shape returned by the query tool.
type queryResult struct {
	Columns   []string         `json:"columns"`
	Rows      []map[string]any `json:"rows"`
	RowCount  int              `json:"row_count"`
	Truncated bool             `json:"truncated"`
}

// validateReadOnly checks that sql is a single SELECT/WITH statement with no
// write or DDL verbs, returning the cleaned statement. Every check runs against
// the statement's executable text, with quoted runs and comments blanked out, so
// what a keyword sits inside decides whether it counts. This is defense in depth
// on top of the read-only connection, not the sole guarantee.
func validateReadOnly(sql string) (string, error) {
	trimmed := strings.TrimSpace(sql)
	trimmed = strings.TrimSuffix(trimmed, ";")
	trimmed = strings.TrimSpace(trimmed)

	if trimmed == "" {
		return "", fmt.Errorf("query is empty")
	}

	code, err := stripQuoted(trimmed)
	if err != nil {
		return "", err
	}
	code = strings.TrimSpace(code)

	if code == "" {
		return "", fmt.Errorf("query is empty")
	}
	if strings.Contains(code, ";") {
		return "", fmt.Errorf("only a single statement is allowed")
	}
	upper := strings.ToUpper(code)
	if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "WITH") {
		return "", fmt.Errorf("query must start with SELECT or WITH")
	}
	if forbiddenPattern.MatchString(code) || replaceStatement.MatchString(code) {
		return "", fmt.Errorf("query contains a forbidden keyword; only read-only SELECT statements are allowed")
	}

	return trimmed, nil
}

// stripQuoted blanks out the parts of a statement that are data rather than
// instruction: string literals, quoted identifiers, and comments. Each is
// replaced by a single space so the tokens around it stay apart. An unclosed
// quote or comment is an error, since the rest of the statement would otherwise
// disappear into it and pass every check unread.
func stripQuoted(statement string) (string, error) {
	var b strings.Builder
	b.Grow(len(statement))

	for i := 0; i < len(statement); {
		switch c := statement[i]; {
		case c == '\'' || c == '"' || c == '`':
			end, ok := skipQuoted(statement, i, c)
			if !ok {
				return "", fmt.Errorf("unterminated %c quote in query", c)
			}
			b.WriteByte(' ')
			i = end
		case c == '[':
			end := strings.IndexByte(statement[i:], ']')
			if end < 0 {
				return "", fmt.Errorf("unterminated [ quote in query")
			}
			b.WriteByte(' ')
			i += end + 1
		case c == '-' && i+1 < len(statement) && statement[i+1] == '-':
			end := strings.IndexByte(statement[i:], '\n')
			if end < 0 {
				b.WriteByte(' ')
				i = len(statement)
				break
			}
			b.WriteByte(' ')
			i += end + 1
		case c == '/' && i+1 < len(statement) && statement[i+1] == '*':
			end := strings.Index(statement[i:], "*/")
			if end < 0 {
				return "", fmt.Errorf("unterminated block comment in query")
			}
			b.WriteByte(' ')
			i += end + len("*/")
		default:
			b.WriteByte(c)
			i++
		}
	}

	return b.String(), nil
}

// skipQuoted returns the index just past the run opened by the quote character
// at start. A doubled quote inside the run is an escaped quote and does not
// close it, which is how SQL escapes both literals and identifiers.
func skipQuoted(statement string, start int, quote byte) (int, bool) {
	for i := start + 1; i < len(statement); i++ {
		if statement[i] != quote {
			continue
		}
		if i+1 < len(statement) && statement[i+1] == quote {
			i++
			continue
		}

		return i + 1, true
	}

	return 0, false
}

// query validates that sql is a single read-only statement, then runs it under a
// deadline with a row cap.
//
// The statement runs inside an explicitly read-only transaction. That is the
// guarantee; the parsing above it is defense in depth, and the pool this borrows
// from already defaults every session to read-only, so a write has to get past
// three separate refusals.
func (d *dbTools) query(ctx context.Context, statement string) (*queryResult, error) {
	trimmed, err := validateReadOnly(statement)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	tx, err := d.db.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("begin read-only transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", queryTimeout.Milliseconds())); err != nil {
		return nil, fmt.Errorf("set statement timeout: %w", err)
	}

	// The statement goes on its own line: a query ending in a -- comment would
	// otherwise swallow the closing paren and fail as incomplete input, which is
	// an error pointing nowhere near the cause.
	wrapped := fmt.Sprintf("SELECT * FROM (\n%s\n) AS _mcp_q LIMIT %d", trimmed, maxRows+1)

	rows, err := tx.Query(ctx, wrapped)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	fields := rows.FieldDescriptions()
	cols := make([]string, len(fields))
	for i, f := range fields {
		cols[i] = f.Name
	}

	out := make([]map[string]any, 0)
	truncated := false
	for rows.Next() {
		if len(out) >= maxRows {
			truncated = true
			break
		}

		vals, err := rows.Values()
		if err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}

		row := make(map[string]any, len(cols))
		for i, c := range cols {
			row[c] = normalizeValue(vals[i])
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}

	return &queryResult{
		Columns:   cols,
		Rows:      out,
		RowCount:  len(out),
		Truncated: truncated,
	}, nil
}

// normalizeValue converts the driver's raw scan values into JSON-friendly forms:
// blobs become strings, timestamps render in RFC 3339, and oversized strings are
// truncated.
func normalizeValue(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case []byte:
		return capString(string(t))
	case string:
		return capString(t)
	case time.Time:
		return t.Format(time.RFC3339Nano)
	default:
		return v
	}
}

func capString(s string) string {
	if len(s) <= maxCellChars {
		return s
	}

	// Cut on a rune boundary: slicing raw bytes split a multi-byte character in
	// half and produced a replacement character at the seam.
	cut := maxCellChars
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}

	return s[:cut] + "…(truncated)"
}

// scanStrings collects a single-column result into a slice, for the
// introspection queries that return one text column per row.
func scanStrings(rows pgx.Rows) ([]string, error) {
	defer rows.Close()

	out := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		out = append(out, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}

	return out, nil
}

// mustJSON marshals v to indented JSON, falling back to an error message rather
// than panicking so a marshal failure surfaces to the caller as text.
func mustJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("failed to encode result: %v", err)
	}

	return string(b)
}
