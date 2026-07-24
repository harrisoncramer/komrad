package mcp

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

// validIdentifier matches the table names we allow callers to describe. Names
// are interpolated into PRAGMA calls and a count, neither of which takes a bound
// parameter, so restricting them to simple identifiers keeps that safe.
var validIdentifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// tableInfo is one row of the list_tables result.
type tableInfo struct {
	Name string `json:"name"`
	Rows int64  `json:"rows"`
}

// columnInfo describes a single column of a table.
type columnInfo struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Nullable   bool   `json:"nullable"`
	Default    string `json:"default,omitempty"`
	PrimaryKey bool   `json:"primary_key,omitempty"`
}

// tableDescription is the describe_table result: columns, foreign keys, indexes.
type tableDescription struct {
	Table       string       `json:"table"`
	Columns     []columnInfo `json:"columns"`
	ForeignKeys []string     `json:"foreign_keys"`
	Indexes     []string     `json:"indexes"`
}

// listTables returns the user tables with their row counts. SQLite keeps no row
// estimate, so each table is counted; the database is small enough that a count
// is cheaper than the caller guessing.
func (d *dbTools) listTables(ctx context.Context) ([]tableInfo, error) {
	names, err := d.tableNames(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]tableInfo, 0, len(names))
	for _, name := range names {
		var count int64
		if err := d.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM "%s"`, name)).Scan(&count); err != nil {
			return nil, fmt.Errorf("count rows of %q: %w", name, err)
		}
		out = append(out, tableInfo{Name: name, Rows: count})
	}

	return out, nil
}

// tableNames lists the tables worth showing: everything but SQLite's own
// bookkeeping and the shadow tables backing a virtual table. The FTS5 search
// index alone owns five shadow tables, none of which is queryable in a useful
// way, so listing them would bury the real schema.
func (d *dbTools) tableNames(ctx context.Context) ([]string, error) {
	const stmt = `
SELECT name, COALESCE(sql, '')
FROM sqlite_master
WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
ORDER BY name`

	rows, err := d.db.QueryContext(ctx, stmt)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var names, virtual []string
	for rows.Next() {
		var name, ddl string
		if err := rows.Scan(&name, &ddl); err != nil {
			return nil, fmt.Errorf("scan table row: %w", err)
		}
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(ddl)), "CREATE VIRTUAL TABLE") {
			virtual = append(virtual, name)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tables: %w", err)
	}

	out := make([]string, 0, len(names))
	for _, name := range names {
		if !isShadowTable(name, virtual) {
			out = append(out, name)
		}
	}

	return out, nil
}

// isShadowTable reports whether name is one of the private tables SQLite creates
// to back a virtual table, which are always the virtual table's name plus a
// suffix.
func isShadowTable(name string, virtual []string) bool {
	for _, v := range virtual {
		if name != v && strings.HasPrefix(name, v+"_") {
			return true
		}
	}

	return false
}

// describeTable returns the columns, foreign keys, and indexes for one table.
func (d *dbTools) describeTable(ctx context.Context, table string) (*tableDescription, error) {
	if !validIdentifier.MatchString(table) {
		return nil, fmt.Errorf("invalid table name %q", table)
	}

	cols, err := d.describeColumns(ctx, table)
	if err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("table %q not found", table)
	}
	fks, err := d.describeForeignKeys(ctx, table)
	if err != nil {
		return nil, err
	}
	indexes, err := d.describeIndexes(ctx, table)
	if err != nil {
		return nil, err
	}

	return &tableDescription{
		Table:       table,
		Columns:     cols,
		ForeignKeys: fks,
		Indexes:     indexes,
	}, nil
}

func (d *dbTools) describeColumns(ctx context.Context, table string) ([]columnInfo, error) {
	rows, err := d.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, fmt.Errorf("describe columns: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]columnInfo, 0)
	for rows.Next() {
		var (
			cid, notNull, pk int
			name, dataType   string
			defaultValue     sql.NullString
		)
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			return nil, fmt.Errorf("scan column row: %w", err)
		}
		out = append(out, columnInfo{
			Name:       name,
			Type:       dataType,
			Nullable:   notNull == 0,
			Default:    defaultValue.String,
			PrimaryKey: pk > 0,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate columns: %w", err)
	}

	return out, nil
}

func (d *dbTools) describeForeignKeys(ctx context.Context, table string) ([]string, error) {
	rows, err := d.db.QueryContext(ctx, fmt.Sprintf("PRAGMA foreign_key_list(%s)", table))
	if err != nil {
		return nil, fmt.Errorf("describe foreign keys: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]string, 0)
	for rows.Next() {
		var (
			id, seq                         int
			target, from, to                string
			onUpdate, onDelete, matchClause string
		)
		if err := rows.Scan(&id, &seq, &target, &from, &to, &onUpdate, &onDelete, &matchClause); err != nil {
			return nil, fmt.Errorf("scan foreign key row: %w", err)
		}
		out = append(out, fmt.Sprintf("%s -> %s.%s", from, target, to))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate foreign keys: %w", err)
	}

	return out, nil
}

// describeIndexes returns the CREATE INDEX statements for a table. Indexes
// SQLite creates itself for a primary key or unique column have no statement, so
// they are skipped rather than reported as blanks.
func (d *dbTools) describeIndexes(ctx context.Context, table string) ([]string, error) {
	const stmt = `
SELECT sql
FROM sqlite_master
WHERE type = 'index' AND tbl_name = ? AND sql IS NOT NULL
ORDER BY name`

	rows, err := d.db.QueryContext(ctx, stmt, table)
	if err != nil {
		return nil, fmt.Errorf("describe indexes: %w", err)
	}

	indexes, err := scanStrings(rows)
	if err != nil {
		return nil, fmt.Errorf("describe indexes: %w", err)
	}

	return indexes, nil
}
