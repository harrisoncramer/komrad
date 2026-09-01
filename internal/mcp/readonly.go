package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"weave/lib/xslice"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	maxRows      = 1000
	maxCellChars = 2000
	queryTimeout = 15 * time.Second
)

var forbiddenPattern = regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE|DROP|ALTER|TRUNCATE|CREATE|GRANT|REVOKE|EXECUTE|CALL|MERGE|VACUUM|REINDEX|ATTACH|DETACH|PRAGMA|BEGIN|COMMIT|ROLLBACK|SET|COPY)\b`)

var replaceStatement = regexp.MustCompile(`(?i)(\A|[(;])\s*REPLACE\s+INTO\b`)

type queryResult struct {
	Columns   []string         `json:"columns"`
	Rows      []map[string]any `json:"rows"`
	RowCount  int              `json:"row_count"`
	Truncated bool             `json:"truncated"`
}

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

	wrapped := fmt.Sprintf("SELECT * FROM (\n%s\n) AS _mcp_q LIMIT %d", trimmed, maxRows+1)

	rows, err := tx.Query(ctx, wrapped)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	cols := xslice.Map(rows.FieldDescriptions(), func(f pgconn.FieldDescription) string { return f.Name })

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

	cut := maxCellChars
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}

	return s[:cut] + "…(truncated)"
}

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

func mustJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("failed to encode result: %v", err)
	}

	return string(b)
}
