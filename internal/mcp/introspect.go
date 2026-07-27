package mcp

import (
	"context"
	"fmt"
	"regexp"
)

// validIdentifier matches the table names we allow callers to describe. Names
// are interpolated into a count, which takes no bound parameter, so restricting
// them to simple identifiers keeps that safe.
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

// tableDescription is the describe_table result, its columns, foreign keys, and
// indexes.
type tableDescription struct {
	Table       string       `json:"table"`
	Columns     []columnInfo `json:"columns"`
	ForeignKeys []string     `json:"foreign_keys"`
	Indexes     []string     `json:"indexes"`
}

// listTables returns the user tables with their row counts. Postgres keeps a
// planner estimate, but it is only as fresh as the last analyze and reads as
// wrong when it disagrees with a count the caller can run themselves, so each
// table is counted. The database is small enough that this is cheap.
func (d *dbTools) listTables(ctx context.Context) ([]tableInfo, error) {
	names, err := d.tableNames(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]tableInfo, 0, len(names))
	for _, name := range names {
		var count int64
		if err := d.db.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %q`, name)).Scan(&count); err != nil {
			return nil, fmt.Errorf("count rows of %q: %w", name, err)
		}
		out = append(out, tableInfo{Name: name, Rows: count})
	}

	return out, nil
}

// tableNames lists the ordinary tables in the public schema. System catalogs
// live in their own schemas and are excluded by that alone, so nothing has to be
// filtered by name.
func (d *dbTools) tableNames(ctx context.Context) ([]string, error) {
	const stmt = `
SELECT table_name
FROM information_schema.tables
WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
ORDER BY table_name`

	rows, err := d.db.Query(ctx, stmt)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}

	names, err := scanStrings(rows)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}

	return names, nil
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

// describeColumns reports each column's type, nullability, default, and whether
// it takes part in the primary key. The primary key arrives as a lateral lookup
// against pg_index rather than a join, so a table without one still returns its
// columns.
func (d *dbTools) describeColumns(ctx context.Context, table string) ([]columnInfo, error) {
	const stmt = `
SELECT
    c.column_name,
    format_type(a.atttypid, a.atttypmod),
    c.is_nullable = 'YES',
    COALESCE(c.column_default, ''),
    COALESCE(pk.is_primary, false)
FROM information_schema.columns c
    JOIN pg_class t ON t.relname = c.table_name AND t.relnamespace = 'public'::regnamespace
    JOIN pg_attribute a ON a.attrelid = t.oid AND a.attname = c.column_name
    LEFT JOIN LATERAL (
        SELECT true AS is_primary
        FROM pg_index i
        WHERE i.indrelid = t.oid AND i.indisprimary AND a.attnum = ANY (i.indkey)
    ) pk ON true
WHERE c.table_schema = 'public' AND c.table_name = $1
ORDER BY c.ordinal_position`

	rows, err := d.db.Query(ctx, stmt, table)
	if err != nil {
		return nil, fmt.Errorf("describe columns: %w", err)
	}
	defer rows.Close()

	out := make([]columnInfo, 0)
	for rows.Next() {
		var col columnInfo
		if err := rows.Scan(&col.Name, &col.Type, &col.Nullable, &col.Default, &col.PrimaryKey); err != nil {
			return nil, fmt.Errorf("scan column row: %w", err)
		}
		out = append(out, col)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate columns: %w", err)
	}

	return out, nil
}

// describeForeignKeys lists the table's outbound foreign keys as "column ->
// target.column".
func (d *dbTools) describeForeignKeys(ctx context.Context, table string) ([]string, error) {
	const stmt = `
SELECT
    src.attname || ' -> ' || target.relname || '.' || tgt.attname
FROM pg_constraint con
    JOIN pg_class source ON source.oid = con.conrelid
    JOIN pg_class target ON target.oid = con.confrelid
    JOIN LATERAL unnest(con.conkey, con.confkey) AS cols (src_attnum, tgt_attnum) ON true
    JOIN pg_attribute src ON src.attrelid = con.conrelid AND src.attnum = cols.src_attnum
    JOIN pg_attribute tgt ON tgt.attrelid = con.confrelid AND tgt.attnum = cols.tgt_attnum
WHERE con.contype = 'f'
    AND source.relnamespace = 'public'::regnamespace
    AND source.relname = $1
ORDER BY 1`

	rows, err := d.db.Query(ctx, stmt, table)
	if err != nil {
		return nil, fmt.Errorf("describe foreign keys: %w", err)
	}

	keys, err := scanStrings(rows)
	if err != nil {
		return nil, fmt.Errorf("describe foreign keys: %w", err)
	}

	return keys, nil
}

// describeIndexes returns the CREATE INDEX statements for a table. pg_indexes
// carries the reconstructed definition, so what comes back is the statement that
// would recreate the index rather than a summary of it.
func (d *dbTools) describeIndexes(ctx context.Context, table string) ([]string, error) {
	const stmt = `
SELECT indexdef
FROM pg_indexes
WHERE schemaname = 'public' AND tablename = $1
ORDER BY indexname`

	rows, err := d.db.Query(ctx, stmt, table)
	if err != nil {
		return nil, fmt.Errorf("describe indexes: %w", err)
	}

	indexes, err := scanStrings(rows)
	if err != nil {
		return nil, fmt.Errorf("describe indexes: %w", err)
	}

	return indexes, nil
}
