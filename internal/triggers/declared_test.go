package triggers

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"testing"

	"komrad/db"
	"komrad/internal/dbtest"
	"komrad/internal/store"
	"komrad/internal/watch"
	"komrad/internal/watched"
	"komrad/lib/xslice"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	rowBit    = 1
	beforeBit = 2
	insertBit = 4
	deleteBit = 8
	updateBit = 16
)

// TestTheSchemaCarriesExactlyTheDeclaredTriggers pins the migrated schema to the declaration, table by table and trigger by trigger.
func TestTheSchemaCarriesExactlyTheDeclaredTriggers(t *testing.T) {
	pool := openPool(t)

	tables := tables()
	assert.Equal(t,
		listStrings(t, pool, `
			SELECT
			    relname::text
			FROM
			    pg_class
			WHERE
			    relnamespace = 'public'::regnamespace
			    AND relkind = 'r'
			    AND relname NOT IN ('schema_migrations', 'schema_triggers')
			ORDER BY
			    relname`),
		xslice.Map(tables, func(table Table) string { return table.Name }),
		"every table in the schema is declared, and only those")

	var declared []string
	for _, table := range tables {
		declared = append(declared, xslice.Map(table.Triggers, func(trigger Trigger) string {
			return spellTrigger(table.Name, trigger.Name, triggerType(t, trigger), trigger.Function, trigger.Args)
		})...)
	}
	slices.Sort(declared)

	rows, err := pool.Query(t.Context(), `
		SELECT
		    c.relname::text,
		    t.tgname::text,
		    t.tgtype::int,
		    p.proname::text,
		    t.tgargs
		FROM
		    pg_trigger t
		    JOIN pg_class c ON c.oid = t.tgrelid
		    JOIN pg_proc p ON p.oid = t.tgfoid
		WHERE
		    c.relnamespace = 'public'::regnamespace
		    AND NOT t.tgisinternal
		    AND t.tgname <> $1`, db.StampEngineTrigger)
	require.NoError(t, err)
	defer rows.Close()

	var migrated []string
	for rows.Next() {
		var table, name, function string
		var tgtype int
		var raw []byte
		require.NoError(t, rows.Scan(&table, &name, &tgtype, &function, &raw))
		var args []string
		if len(raw) > 0 {
			for arg := range bytes.SplitSeq(bytes.TrimSuffix(raw, []byte{0}), []byte{0}) {
				args = append(args, string(arg))
			}
		}
		migrated = append(migrated, spellTrigger(table, name, tgtype, function, args))
	}
	require.NoError(t, rows.Err())
	slices.Sort(migrated)

	assert.Equal(t, declared, migrated, "the schema's triggers drifted from the declaration; run mise run gen:triggers")
}

// TestTheSchemaCarriesExactlyTheDeclaredFunctions pins every trigger function in the schema to its declared body.
func TestTheSchemaCarriesExactlyTheDeclaredFunctions(t *testing.T) {
	pool := openPool(t)

	rows, err := pool.Query(t.Context(), `
		SELECT
		    proname::text,
		    prosrc
		FROM
		    pg_proc
		WHERE
		    pronamespace = 'public'::regnamespace
		    AND prorettype = 'trigger'::regtype
		    AND proname <> $1
		ORDER BY
		    proname`, db.StampEngineTrigger)
	require.NoError(t, err)
	defer rows.Close()

	var migrated []Function
	for rows.Next() {
		var fn Function
		require.NoError(t, rows.Scan(&fn.Name, &fn.Body))
		migrated = append(migrated, fn)
	}
	require.NoError(t, rows.Err())

	assert.Equal(t, Functions(), migrated, "the schema's trigger functions drifted from the declaration; run mise run gen:triggers")
}

// TestEveryDeclaredFunctionIsRun pins that no declared function is left without a trigger running it.
func TestEveryDeclaredFunctionIsRun(t *testing.T) {
	run := map[string]bool{}
	for _, table := range tables() {
		for _, trigger := range table.Triggers {
			run[trigger.Function] = true
		}
	}

	for _, fn := range Functions() {
		assert.True(t, run[fn.Name], "%s is declared but no trigger runs it", fn.Name)
	}
}

// TestEveryUpdatedAtColumnIsStamped pins set_updated_at onto every table carrying updated_at.
func TestEveryUpdatedAtColumnIsStamped(t *testing.T) {
	pool := openPool(t)

	stamped := map[string]bool{}
	for _, table := range tables() {
		stamped[table.Name] = slices.ContainsFunc(table.Triggers, func(trigger Trigger) bool {
			return trigger.Name == setUpdatedAt.Name && trigger.Function == setUpdatedAt.Function
		})
	}

	columns := listStrings(t, pool, `
		SELECT
		    table_name::text
		FROM
		    information_schema.columns
		WHERE
		    table_schema = 'public'
		    AND column_name = 'updated_at'`)
	require.NotEmpty(t, columns)

	for _, table := range columns {
		assert.True(t, stamped[table], "%s has updated_at and no set_updated_at trigger", table)
	}
}

// TestEveryWatchedColumnIsInTheSchema pins that every column a watched resource names exists on its table.
func TestEveryWatchedColumnIsInTheSchema(t *testing.T) {
	pool := openPool(t)

	migrated := map[string]bool{}
	for _, column := range listStrings(t, pool, `
		SELECT
		    table_name::text || '.' || column_name::text
		FROM
		    information_schema.columns
		WHERE
		    table_schema = 'public'`) {
		migrated[column] = true
	}

	for _, column := range listWatchedColumns(watched.All()) {
		assert.True(t, migrated[column], "%s is watched but not in the schema", column)
	}
}

func openPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	st, err := store.Open(t.Context(), dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	return st.GetRawPool()
}

func listStrings(t *testing.T, pool *pgxpool.Pool, query string) []string {
	t.Helper()

	rows, err := pool.Query(t.Context(), query)
	require.NoError(t, err)
	defer rows.Close()

	var values []string
	for rows.Next() {
		var value string
		require.NoError(t, rows.Scan(&value))
		values = append(values, value)
	}
	require.NoError(t, rows.Err())

	return values
}

func triggerType(t *testing.T, trigger Trigger) int {
	t.Helper()

	tgtype := rowBit
	if trigger.Timing == "BEFORE" {
		tgtype |= beforeBit
	}
	bits := map[string]int{"INSERT": insertBit, "UPDATE": updateBit, "DELETE": deleteBit}
	for event := range strings.SplitSeq(trigger.Events, " OR ") {
		bit, ok := bits[event]
		require.True(t, ok, "trigger %s has event %q", trigger.Name, event)
		tgtype |= bit
	}

	return tgtype
}

func spellTrigger(table, name string, tgtype int, function string, args []string) string {
	return fmt.Sprintf("%s.%s type=%d %s(%s)", table, name, tgtype, function, strings.Join(args, ", "))
}

func listWatchedColumns(resources []watch.Resource) []string {
	var columns []string
	named := func(table, column string) {
		if column != "" {
			columns = append(columns, table+"."+column)
		}
	}
	for _, resource := range resources {
		columns = append(columns, resource.Table+"."+resource.KeyColumn)
		for _, ignored := range resource.IgnoredColumns {
			named(resource.Table, ignored)
		}
		for _, feeder := range resource.Feeders {
			named(feeder.Table, feeder.Column)
			if lookup := feeder.Lookup; lookup != nil {
				named(resource.Table, lookup.ParentColumn)
				named(resource.Table, lookup.SkipParentsWith)
				named(feeder.Table, lookup.ChildColumn)
				named(feeder.Table, lookup.OnChangeOf)
			}
		}
	}

	return columns
}
